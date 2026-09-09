
# atvpn 初期構想

ATProtocolを制御プレーンとして使うVPNのアイデア。Relay/AppViewには依存せず、DID resolve → record fetchだけで完結させる方針。

## 基本設計

- アイデンティティの単位はDID
- ユーザーは単一Record `blue.maril.atvpn.config/self` を持ち、そこにデバイスごとの公開鍵をぶら下げる
- パッケージは2つに分割
  - **core**: wireguard-goベース、Go。ATProtocolの処理は持たず、純粋なHTTPでdid resolve → record fetchのみ行う
  - **web**: atcuteベース、Deno + TypeScript。ATProtocol OAuth等の複雑な処理はすべてこちらに寄せる

## 議論した論点1: 鍵登録のUX

`atvpn up` 時に鍵の自動登録までやろうとすると、coreがATProtocolのOAuthを扱う必要が出て面倒。単純なコピペ運用も考えたが、以下のペアリングフローで解決する方向にした。

最初はデバイスコードフロー（`gh auth login`等と同じ、ランダムなpairingコードを発行する方式）を検討したが、そもそも公開鍵は秘密情報ではなく最終的に`config/self`で公開される値なので、ランダムなセッションコードを別途発行する必要はないという結論に至った。ただし公開鍵をそのままURLに乗せる場合、攻撃者が`/pair?pk=<攻撃者の鍵>`のようなリンクを作って被害者に踏ませ、確認画面でOKを押させるだけで攻撃者の鍵を登録させられてしまう（device code phishingと同種の問題）。これはランダムコードを使っても「意味の分からない文字列を目視なしで確認するだけ」なら同じく防げないため、**pk自体から決定的に導出した指紋の目視確認**で対応することにした。

1. `atvpn up` 実行時、coreがローカルでデバイス鍵ペアを生成
2. coreは鍵から指紋（例: `sha256(pk)`の先頭数文字、`4F2A-91BC`のような形式）を計算し、ターミナルに表示
3. coreはブラウザで `https://web/pair?pk=<pubkey>` を開く（**このURLに渡すpkは秘密情報ではないので、そのまま乗せてよい**）
4. webは通常のログイン処理を行う（未ログインならATProtocolのOAuthでハンドル入力から、既にブラウザにセッションがあればそれを再利用する）
5. webの確認画面に「このアカウント（handle）にこのデバイスを登録します」という表示と、pkから計算した指紋を表示。ユーザーはターミナルとブラウザの指紋が一致するか目視確認してからOKを押す
6. webがpkを、ログイン中のアカウントの `config/self` のdevices配列に `putRecord`
7. coreはターミナルに「登録が完了したらEnterキーを押してください」と表示し、標準入力を待つ
8. Enterが押されたら、**その場で1回だけ** `did resolve + getRecord` を行い、自分のpkが `config/self` に登場しているか確認する。見つかれば登録完了として終了し、見つからなければ「まだ確認できませんでした、ブラウザでの操作を終えてからもう一度Enterキーを押してください」と表示して再度待つ

ブラウザでの操作が完了するタイミングはユーザー本人が一番正確に知っているため、間隔やタイムアウトを見積もる必要がある常時ポーリング方式や、専用のローカルHTTPサーバーを立てて完了通知を受け取るコールバック方式は不要と判断した。特にコールバック方式は、core・ブラウザが同一マシン上にある前提でしか成立せず、ヘッドレスな環境（サーバーやルーターでcoreを動かし、ログイン確認は別端末のブラウザで行う）に対応できないため見送った。Enter待ち方式ならこの制約もなく、実装も一番薄い。

`did`をURLに含める案も検討したが、「ハンドル入力を求めるログイン画面はATProtocolのOAuthとしてはそもそも普通の体験」「既存セッションがあればそれが使われるだけ」という理由で不要と判断し外した。ただし`core`自身は、手順8の確認のために「自分がどのDIDか」を別途知っておく必要があり、これは`atvpn login <handle>`のようなCLI側の初期設定で別途解決する話として切り分けた。webとcoreで対象アカウントがズレた場合は、手順5の確認画面表示でユーザーが気づける。

これにより、webに残る新規エンドポイントは`/pair`の画面表示と確認ボタンのハンドラのみになり、pairing専用の状態（コードとその有効期限など）を一切持たずに済む。「ATProtocolの複雑さは全部web」「coreはHTTPクライアントとしてしか動かない」という役割分担もそのまま維持できる。

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

