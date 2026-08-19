# sitka

Claude Code のサブエージェントを、Anthropic 以外のモデルで動かすためのローカルゲートウェイ。

`ANTHROPIC_BASE_URL` を sitka に向けると、Claude Code のリクエストはモデル ID で振り分けられる。

```
Claude Code ──▶ sitka (127.0.0.1:8787) ──┬── claude-*         ──▶ api.anthropic.com（無改変で転送）
                                          └── openai-gpt-5.2  ──▶ OpenAI 互換 API（形式変換）
```

`claude-` で始まる ID を含め、設定したプロバイダのプレフィックスに一致しないモデルは
すべて Anthropic 公式 API へそのまま流れる。ヘッダもボディも 1 バイトも書き換えないので、
Claude Code が新しいベータ機能を使い始めても sitka を更新する必要はない。

sitka は Anthropic の API キーを持たない。転送時はクライアントが送ってきた
`x-api-key` / `Authorization` をそのまま使うので、claude.ai のサブスクリプションログインの
ままでも動く。設定ファイルに書くのは外部プロバイダの鍵だけ。

## セットアップ

```bash
make build            # bin/sitka
```

`~/.config/sitka/config.yaml` を用意する。

```yaml
listen: 127.0.0.1:8787

anthropic:
  base_url: https://api.anthropic.com

providers:
  - id: openai
    base_url: https://api.openai.com/v1
    api_key: sk-...
```

`id` は `[a-z0-9]+` のみ。モデル ID を最初の `-` で分割してプロバイダを決めるため、
`id` に `-` は使えない。`claude` と `anthropic` は予約語。

`base_url` を差し替えれば OpenRouter・Groq・DeepSeek・xAI・Ollama など
Chat Completions 互換の API はそのまま使える。

```yaml
providers:
  - id: openrouter
    base_url: https://openrouter.ai/api/v1
    api_key: sk-or-...
  - id: ollama
    base_url: http://127.0.0.1:11434/v1
    api_key: dummy
```

起動する。

```bash
sitka serve
```

Claude Code を sitka 経由で立ち上げる。

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8787
claude
```

## サブエージェントで外部モデルを使う

使えるモデル ID を確認する。

```bash
sitka models
```

`.claude/agents/reviewer.md` の frontmatter にその ID を書く。

```markdown
---
name: reviewer
description: 別モデルの視点でコードレビューする
model: openai-gpt-5.2
---

変更差分をレビューして、バグと設計上の問題を指摘してください。
```

`ANTHROPIC_BASE_URL` が設定されている間、Claude Code はモデル ID を検証せずそのまま送るので、
このサブエージェントのリクエストだけが OpenAI へ流れる。メインの会話は Anthropic のままになる。

## `ANTHROPIC_BASE_URL` はグローバルに設定しない

この変数を設定している間、Claude Code は **Remote Control を無効化する**（v2.1.196 以降）。
tool search と違って復帰用の変数はなく、変数を外してセッションを開き直すしかない。
シェルの profile や `settings.json` の `env` に置くと全セッションで効いてしまうので、
sitka を使いたいセッションだけで指定する。

```bash
ANTHROPIC_BASE_URL=http://127.0.0.1:8787 claude
```

なお `settings.json` の `env` はシェルの `export` を上書きする。設定が効かないときは
まずそちらを疑う。

## 既知の制限

- **`/model` ピッカーには自動登録されない。** Claude Code のゲートウェイモデル検出は、
  ID に `claude` か `anthropic` を含むものしか拾わない仕様のため、`openai-*` は出てこない。
  メインの会話で使いたい場合は `ANTHROPIC_CUSTOM_MODEL_OPTION=openai-gpt-5.2` を設定する。
- **`/v1/messages/count_tokens` は非 Anthropic モデルに対して 404 を返す。**
  OpenAI 互換 API に正確なトークン数を数える手段がなく、推定でごまかすことはしない。
  Claude Code は推論エンドポイント経由の計測に切り替わる。
- **attribution ブロックがプロンプトに含まれる。** Claude Code はシステムプロンプトの先頭に
  クライアント情報のブロックを付ける。Anthropic 公式 API はこれを除去するが、他のプロバイダは
  プロンプトの一部として受け取る。気になる場合は `CLAUDE_CODE_ATTRIBUTION_HEADER=0` を設定する。
- **コンテキスト長の仮定がずれる。** Claude Code は知らないモデル ID に対して独自の
  コンテキスト長を仮定するので、実際と違う場合は `CLAUDE_CODE_MAX_CONTEXT_TOKENS` で宣言する。
- **`thinking` ブロックは変換時に落とす。** OpenAI 互換 API に対応物がないため。
- **`ENABLE_TOOL_SEARCH=true` にしない。** `ANTHROPIC_BASE_URL` 配下では MCP の tool search が
  既定で無効になる。理由は「多くのプロキシが `tool_reference` ブロックを転送しないから」で、
  sitka の Anthropic 転送は無改変なので本来は有効化できる。ただし有効にすると
  `tool_reference` ブロックが非 Anthropic モデルにも流れ、対応物がないため 400 になる。
  MCP ツールを持つサブエージェントを外部モデルで動かすなら、既定の無効のままにする。
- **`ANTHROPIC_DEFAULT_HAIKU_MODEL` に外部モデルを割り当てない。** この alias は会話タイトル生成
  などのバックグラウンド処理にも使われるので、意図しない場所で外部プロバイダに課金が発生する。

## 開発

```bash
make test    # go test ./...
make lint    # golangci-lint run
```

Nix flake の devShell に Go とツールが入っている。`direnv allow` するか `nix develop` する。
