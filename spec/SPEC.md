
# atvpn 初期構想

ATProtocolを制御プレーンとして使うVPNのアイデア。Relay/AppViewには依存せず、DID resolve → record fetchだけで完結させる方針。

## 基本設計

- アイデンティティの単位はDID
- ユーザーは単一Record `blue.maril.atvpn.config/self` を持ち、そこにデバイスごとの公開鍵をぶら下げる
- パッケージは2つに分割
  - **core**: wireguard-goベース、Go。ATProtocolの処理は持たず、純粋なHTTPでdid resolve → record fetchのみ行う
  - **web**: atcuteベース、Deno + TypeScript。ATProtocol OAuth等の複雑な処理はすべてこちらに寄せる

## 議論した論点1: 鍵登録のUX

`atvpn up` 時に鍵の自動登録までやろうとすると、coreがATProtocolのOAuthを扱う必要が出て面倒。単純なコピペ運用も考えたが、以下のデバイスコードフロー方式（`gh auth login`等と同じパターン）で解決する方向にした。

1. `atvpn up` 実行時、coreがローカルでデバイス鍵ペアを生成
2. coreはwebが持つ**ATProtocolとは無関係な素のHTTPエンドポイント**（`POST /pair`など）に公開鍵とワンタイムのpairingコードを送る
3. ブラウザで `https://web/pair?code=xxx` を開く
4. webがATProtocol OAuthを一手に引き受けてユーザーのPDSにログイン
5. webがpairingコードに紐づく公開鍵を `config/self` のdevices配列に `putRecord`
6. coreは `/pair` エンドポイントをポーリングし、成功したら通常のdid resolve + getRecordのフローに合流

これにより「ATProtocolの複雑さは全部web」「coreはHTTPクライアントとしてしか動かない」という役割分担を崩さずに済む。

前提: `config/self` は認証なしの公開recordとして読める（PDSのデフォルト）。非公開のpermissioned dataはまだアルファなので依存しない。

## 議論した論点2: IPの割り振り

最初はgroupレコード（`blue.maril.atvpn.group/hogepiyo`、VPN管理者が作成）にDID単位でIPを書く案を検討したが、以下の理由で「device単位」に修正した。

- IPはアイデンティティの属性ではなく、「そのDIDがどのVPNグループに所属しているか」という関係の属性
- DIDは比較的不変なものとして扱われるべきで、groupごとに変わるIPをconfig/self側に書くのは責務が違う
- WireGuard(wireguard-go含む)のAllowedIPsは内部的にtrie構造で「1つのIPプレフィックスは1つのpeerにしか属せない」という制約がある。同じDIDの複数デバイス鍵を同一IPに割り当てると、後から追加した鍵が所有権を奪って前のデバイスが弾き出される事故になる

Tailscaleの設計を参考に確認した。Tailscaleはidentity（アカウント）を認可レイヤーの話に閉じていて、アドレッシングの単位は常にdevice。1アカウントに複数デバイスがぶら下がっていても、各デバイスは独立したWireGuard鍵と独立したTailscale IP(100.64.0.0/10)を持つ。coordination serverが全peer分のkey+IPのnetmapを配布する。

この対応関係:

| Tailscale | atvpn |
|---|---|
| identity（アカウント） | `config/self`（DID） |
| device（node） | deviceId + 公開鍵 |
| coordination serverが配るnetmap | groupレコード |

よって、groupレコードのスキーマはdevice単位が妥当という結論。折衷案（`did` + IPレンジだけを書き、core側がdevices配列の並び順等から決定的にIPを割り振る）も検討したが、**device単位で列挙する方式に決定**した。

```
blue.maril.atvpn.group/hogepiyo (adminのrepoに存在)
  members:
    - did: did:plc:xxxx
      deviceId: laptop-1   # このDIDのconfig/selfのdevices配列の中の特定の1台を指す
      ip: 10.0.0.2
    - did: did:plc:xxxx
      deviceId: phone-1     # 同じDIDでも別デバイスなら別エントリ
      ip: 10.0.0.3
    - did: did:plc:yyyy
      deviceId: laptop-1
      ip: 10.0.0.4
```

groupレコードの1エントリ＝1デバイス。coreはこれを見て各エントリの`did`を解決して`config/self`を取得し、`deviceId`に一致する公開鍵を抜き出して`(pubkey, ip)`のペアをそのままWireGuardのピアとして登録する。

利点は単純明快で衝突しないこと。欠点はデバイスが増えるたびに管理者がgroupレコードにエントリを1個ずつ手で足す必要があること。

## 議論した論点3: group参加時の招待コード

group URIは `at://<authority>/<collection>/<rkey>` の形式だが、collection名(`blue.maril.atvpn.group`)は固定なので、実質可変なのは `did` と `rkey` の2つだけ。

Tailscaleが最近出したOSSツール [tailcat](https://github.com/tailscale/tailcat) を参考にした。tailcatは共有された永続状態を持たない完全な1:1ペアリングのため、鍵とDERP情報を `"tc" + base64(CBOR)` という1本の文字列に折りたたんでサーバー側が生成し、帯域外でクライアントに渡す設計になっている。

これを踏まえて、atvpnでは以下の形式に決定した。

```
code = "atv1" + base64url(f"{did}/{rkey}")
```

- プレフィックス `atv1` はtailcatの `"tc"` を参考に、コード種別の判定用
- `core`は `atvpn join <code>` でこれを受け取り、デコードして `/` で分割し `did` と `rkey` を復元する（did:webでも生の `/` はエンコードされないため安全に分割できる）
- invite codeにDERP情報などのグループ全体設定は含めない（論点4を参照）。招待のたびに変わらない情報は招待コードではなくgroupレコード側に持たせる

## 議論した論点4: NAT越え

初期段階ではホールパンチ（STUNベースのendpoint探索、keepalive等）を実装せず、DERPで常時リレーする方針にした。実装コストが高い割に、後から「直接経路が確立できたら乗り換える」形で拡張できるため、最初はシンプルさを優先する。

- リレーサーバーのアドレスは招待コードではなく **groupレコードのフィールド**（例: `relay: "wss://relay.example.com"`）に持たせる。理由は論点3のtailcatとの違いと同じで、atvpnには既にgroupレコードという共有状態があるため、招待のたびに変わらないグループ全体の設定はそちらに寄せた方が、リレー変更時に発行済みコードが陳腐化しない
- `web`はリレーに一切関与しない。`core`がDERPプロトコルを直接喋ってリレーサーバーに接続する（役割分担`web`＝ATProtocol専任、を崩さない）
- リレーサーバー自体の運用は帯域外の話。初期は自前でホストせず、tailcatが提供する無料・レート制限ありのDERPリレー（認証不要、[https://tailcat.dev/derpmap.json](https://tailcat.dev/derpmap.json) のマップを参照）を使う選択肢がある。将来的に自前で`derper`をホストする選択肢も残す

## 未決事項

特になし（上記論点3で解決済み）。運用が進んだ段階でリレーのレート制限に達した場合の対応方針は将来課題として残る。

