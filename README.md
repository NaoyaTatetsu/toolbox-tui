# toolbox-tui

GitHub Project（Projects v2）と Google Calendar をターミナルで扱うタスク管理 TUI。
コマンド名は `tt`。
Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea) 製。

- **Board** — Status 列ごとのカンバン。カードの移動でそのまま GitHub の Status を更新
- **Roadmap** — Start Date / End Date のガントチャート。ズームとスクロール対応
- **Calendar** — 月グリッド + 日別アジェンダ。Google Calendar の予定だけを表示（タスクは Board / Roadmap 側）
- **タスク登録** — タイトル / 内容 / ラベル / 期日 / 優先度 を入力して Issue を作成し、Project に追加してフィールドを設定

## インストール

```sh
make install     # ~/.local/bin/tt に入れる（PATH の優先度が高い方）
# GOBIN に入れたい場合
make install-gobin
```

**注意:** `go install` は `$(go env GOBIN)` に入れるが、`~/.local/bin` の方が
`$PATH` で先にある環境では古いコピーが新しいビルドを黙って隠す。実行中のバイナリは
`tt version` で確認できる。

```sh
$ tt version
tt
  build   2026-08-29 00:35:42
  binary  /Users/you/.local/bin/tt
```

インストール先を変えるには `make install INSTALL_DIR=/somewhere/bin`。

## セットアップ

### 1. 設定ファイル

```sh
tt init      # ~/.config/toolbox-tui/config.toml を生成
```

```toml
[github]
owner          = "your-github-login"     # プロジェクト URL の users/<owner>
owner_type     = "user"                  # "user" か "organization"
project_number = 4                       # プロジェクト URL の projects/<number>
default_repo   = "your-name/notes"        # 新規タスクの Issue 作成先

[ui]
status_order = ["Pending", "Todo", "In Progress", "In Review", "Done"]
hide_done    = false
roadmap_days = 28
mouse              = true  # トラックパッド / ホイールスクロール
scroll_ticks       = 2     # 1ステップに必要なホイールイベント数
scroll_interval_ms = 60    # ステップ間の最小間隔（ミリ秒）

[[calendar.sources]]
name  = "personal"
url   = "https://calendar.google.com/calendar/ical/…/private-…/basic.ics"
color = "#7aa2f7"
```

`default_repo` は必須ではないが、**ラベルは Issue にしか付けられない**ため、
ラベル付きのタスク登録にはこれが必要。未設定だとタスク登録が無効になる。

### 2. GitHub トークン

`github.token` → `$GITHUB_TOKEN` / `$GH_TOKEN` → `gh auth token` の順に探索する。
`gh` でログイン済みなら追加設定は不要。ただし **書き込みには `project` スコープが必要**:

```sh
gh auth refresh -s project,read:project,repo
```

`read:project` だけだと閲覧はできるがタスク登録と Status 変更が失敗する。

### 3. Google Calendar

Google Calendar の「設定 → 対象のカレンダー → **iCal 形式の非公開 URL**」をコピーして
`[[calendar.sources]]` に貼る。カレンダーごとにブロックを繰り返せば複数表示できる。
この URL は秘密情報なので、設定ファイルは `0600` で作成される。

### 4. 動作確認

```sh
tt doctor
```

設定・トークンのスコープ・プロジェクトのフィールド・カレンダーの疎通をまとめて検査する。

## キーバインド

移動はすべて**矢印キー**。`h` `j` `k` `l` は割り当てていない。
トラックパッドの2本指スクロール（およびマウスホイール）も上下左右の矢印と同じ動作をする。

### 全体

| キー | 動作 |
| --- | --- |
| `[` `]` | 前 / 次のビュー（Board ⇄ Roadmap ⇄ Calendar） |
| `1` `2` `3` | Board / Roadmap / Calendar を直接指定 |
| `n` | タスク登録 |
| `,` `.` | スクロール感度を下げる / 上げる |
| `r` | 再読み込み |
| `?` | ヘルプ |
| `q` | 終了 |

`tab` / `shift+tab` も `]` / `[` と同じ動作。

### Board

| キー | 動作 |
| --- | --- |
| `←` `→` | 列の移動 |
| `↑` `↓` | カードの移動 |
| `H` `L` | カードを隣の Status へ移動（GitHub に反映） |
| `home` `end` | 先頭 / 末尾のカード |
| `enter` | 詳細 |
| `o` | ブラウザで開く |

### Roadmap

| キー | 動作 |
| --- | --- |
| `↑` `↓` | タスク選択 |
| `←` `→` | タイムラインをスクロール |
| `-` `+` | ズームアウト / イン（14 / 28 / 56 / 112 日） |
| `t` | 今日へ |
| `f` | 選択中タスクの開始日へ |
| `home` `end` | 先頭 / 末尾のタスク |
| `enter` | 詳細 |

### Calendar

| キー | 動作 |
| --- | --- |
| `←` `→` | 前日 / 翌日 |
| `↑` `↓` | 前週 / 翌週 |
| `H` `L` | 前月 / 翌月 |
| `J` `K` | アジェンダ内の選択（`shift+↓` `shift+↑` も同じ） |
| `t` | 今日へ |

### タスク登録フォーム

| キー | 動作 |
| --- | --- |
| `tab` / `shift+tab` | フィールド移動 |
| `←` `→` | Status / Priority の選択、ラベルのカーソル移動 |
| `space` | ラベルの ON/OFF |
| `ctrl+s` | 作成 |
| `esc` | キャンセル |

日付欄は `2026-09-01` のほか `today` / `tomorrow` / `+3d` / `+2w` / `+1m` を受け付ける。

フォームと詳細表示が開いている間は `[` `]` `1` `2` `3` はビュー切り替えに使われない
（フォームでは文字として入力される）。`esc` で閉じてから切り替える。

### 詳細表示

| キー | 動作 |
| --- | --- |
| `↑` `↓` | 本文をスクロール |
| `o` | ブラウザで開く |
| `esc` | 閉じる |

### トラックパッド / マウス

| 操作 | 動作 |
| --- | --- |
| クリック | クリックしたものを選択 |
| 上下スクロール | `↑` `↓` と同じ |
| 左右スクロール | `←` `→` と同じ |
| `shift` + 上下スクロール | `←` `→`（左右スクロールを送らない端末向けのフォールバック） |

クリックの対象はビューごとに以下。選択のみで、開くのは `enter`。

| ビュー | クリック対象 |
| --- | --- |
| Board | カード |
| Roadmap | タスクの行 |
| Calendar | 月グリッドの日付セル（前後の月の日付も可）、アジェンダの行 |

オーバーレイ（詳細・フォーム・ヘルプ）が開いている間はクリックは無効。

左右スクロールは端末が SGR のボタン 6 / 7 を送る必要がある。iTerm2 / Ghostty /
WezTerm / Alacritty / Kitty は送るが、macOS の Terminal.app は送らないので、
その場合は `shift` + 上下スクロールを使う。

**感度は TUI 上で `,` / `.` で調整する。** 数値を config で悩むより速い。押すと
プリセットを1段ずつ移動し、ステータス行に到達した段と config へ貼る値が出る。

```
scroll 5/6 (max 33/s) — scroll_ticks = 1, scroll_interval_ms = 30
```

気に入った値を `config.toml` に書けば次回から既定になる。

| 段 | `scroll_ticks` | `scroll_interval_ms` | 上限 |
| --- | --- | --- | --- |
| 1 | 6 | 200 | 5 ステップ/秒 |
| 2 | 4 | 140 | 7 |
| 3 | 3 | 100 | 10 |
| **4（既定）** | **2** | **60** | **16** |
| 5 | 1 | 30 | 33 |
| 6 | 1 | 0 | 制限なし（マウスホイール向け） |

抑制が2段構えなのはトラックパッドに両方必要だから。**指を離した後も慣性で
イベントが飛び続ける**のでイベント数の閾値だけでは止まらず、`scroll_interval_ms`
がステップの発生頻度自体に上限をかける。クールダウン中のイベントは蓄積せずに
破棄するので、慣性が収まった後にまとめて動くことはない。逆方向スクロールや軸の
切り替えでもカウントを破棄するため、切り返しの反応は鈍らない。

マウス報告モードを有効にすると端末側のドラッグでの範囲選択が効かなくなるため、
無効化したい場合は設定で切れる。

```toml
[ui]
mouse = false
```

## 秘匿情報の保存場所

| 情報 | 保存場所 | 権限 | 備考 |
| --- | --- | --- | --- |
| GitHub トークン | **このツールは保存しない** | — | 起動ごとに `gh auth token` を実行して取得。実体は `gh` が macOS キーチェーンに保持 |
| Google Calendar の iCal 非公開 URL | `~/.config/toolbox-tui/config.toml` | `0600` | **平文**。この URL 自体が認証情報 |
| Issue のタイトル・本文、予定名・場所 | `$XDG_CACHE_HOME/toolbox-tui/*.json` | `0600` | 認証情報は含まない |

トークンの探索順は `github.token`（config）→ `$GITHUB_TOKEN` / `$GH_TOKEN` →
`gh auth token`。`github.token` に直接書けば config に平文で残るので、通常は
書かずに `gh` に任せる方がよい。

ICS の非公開 URL は Bearer トークンと同等で、**URL を持っている者は誰でも
カレンダーを読める**。config を共有リポジトリに置いたり、画面共有で開いたりしない。
再発行するとカレンダー設定側で URL がローテートされる。

エラーメッセージ中の URL はスキームとホストだけに縮める。`net/http` は
トランスポートエラーに URL 全体（秘密トークン込み）を埋め込むので、
そのまま表示すると画面と `tt doctor` の出力に漏れる。

```
personal: Get "https://calendar.google.com/…": dial tcp: no such host
```

キャッシュは中身を見て問題があれば捨てられる。

```sh
tt cache clear
```

## 起動速度

初回フレームはディスクキャッシュから描画し、ネットワークの取得は裏で走らせて
差し替える。実測（60 タスク / 2130 予定）:

| | 時間 |
| --- | --- |
| キャッシュあり → 操作可能な初回フレーム | **10 ms** |
| キャッシュなし → Board 操作可能 | 1.8 秒 |
| キャッシュなし → Calendar 操作可能 | 6.1 秒 |

Calendar が遅いのは Google 側のレスポンスが遅いため。gzip は既に効いていて
(5.0 MB → 437 KB)、`curl` でも同じだけかかる。`cache-control: no-cache, no-store`
で ETag も返らないので条件付きリクエストも使えない。キャッシュ以外に手が無い。

キャッシュは `$XDG_CACHE_HOME/toolbox-tui/`（macOS では `~/Library/Caches/toolbox-tui/`）。
issue のタイトルや予定名が入るので `0600` で書く。おかしくなったら捨てられる。

```sh
tt cache clear
```

キャッシュ由来のデータを表示している間はフッターが `refreshing` になり、
ステータス行に取得時刻が出る。ネットワークの応答が来たら差し替わる。
取得に失敗した場合はキャッシュの内容を残す（画面が空にならないように）。

## 設計メモ

- プロジェクトのフィールド定義は起動時に取得し、`Status` と `Priority` の選択肢は
  **実際のプロジェクトから読む**。フォームがサーバーに存在しない値を送ることはない。
- `Status` / `Priority` / `Start Date` / `End Date` が無いプロジェクトでも起動する。
  該当する UI が空になるだけで、`doctor` が何が欠けているかを報告する。
- Board のカード移動は楽観的に反映し、失敗はステータス行に出したうえで次の再読み込みで
  サーバーの状態に戻る。
- カレンダーは iCalendar を自前で展開する。`RRULE` の繰り返し、`EXDATE` の除外、
  `RECURRENCE-ID` による個別変更、終日イベントの排他的 `DTEND` に対応。
- 秘密 URL はエラーメッセージ中でマスクされる。
- Project のアイテム取得は 1 ページ 100 件（GitHub の上限）。フィールド定義は
  `@include` で 2 ページ目以降のクエリから外している。
- クリックの当たり判定は**描画時にレンダラが記録した矩形**を引く。クリック
  ハンドラ側で列幅・スクロール位置・行高を再計算するとレイアウトと二重管理に
  なって食い違うため。画面に実際に出ていたものにしか当たらない。

## 開発

```sh
make test     # 単体テスト
make vet
make build
make frames   # 実プロジェクトに接続して各ビューを 1 フレーム描画
```

テストは既定で**ネットワークも認証情報も不要**。GitHub と Google Calendar に
実際に接続するテストは `LIVE=1` を付けたときだけ動き、それ以外はスキップする。

```sh
LIVE=1 go test ./internal/ui -run TestLiveRender -v    # 各ビューを実データで描画
LIVE=1 go test ./internal/ui -run TestStartupTiming -v # 起動時間の内訳
```

`LIVE=1` のテストは `~/.config/toolbox-tui/config.toml` を読むので、自分の
プロジェクトに向けて実行することになる。CI では動かない。

## ライセンス

MIT。直接依存（bubbletea / lipgloss / bubbles / x/ansi / rrule-go /
BurntSushi/toml）はすべて MIT。
