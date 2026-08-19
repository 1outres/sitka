# sitka

Claude Codeのサブエージェントを、Anthropic以外のモデルで動かすためのローカルゲートウェイ。

`ANTHROPIC_BASE_URL`をsitkaに向けると、リクエストはモデルIDで振り分けられる。

```
Claude Code ──▶ sitka (127.0.0.1:8787) ──┬── claude-*         ──▶ api.anthropic.com（無改変で転送）
                                          └── openai-gpt-5.2  ──▶ OpenAI互換API（形式変換）
```

プロバイダのプレフィックスに一致しないモデルIDは、すべてAnthropic公式APIへそのまま流れる。ヘッダもボディも書き換えない。

sitkaはAnthropicのAPIキーを持たない。転送時はクライアントが送ってきた`x-api-key`と`Authorization`をそのまま使うので、claude.aiのサブスクリプションログインのままで動く。設定ファイルに書くのは外部プロバイダの鍵だけ。

## 起動

```bash
make build            # bin/sitka
```

`~/.config/sitka/config.yaml`を用意する。

```yaml
listen: 127.0.0.1:8787

anthropic:
  base_url: https://api.anthropic.com

providers:
  - id: openai
    base_url: https://api.openai.com/v1
    api_key: sk-...
```

`id`がモデルIDのプレフィックスになる。使えるのは`[a-z0-9]+`だけで、`claude`と`anthropic`は予約語。`base_url`を差し替えればOpenRouterやGroq、Ollamaなど、Chat Completions互換のAPIはそのまま使える。

```bash
sitka serve
```

Claude Codeをsitka経由で立ち上げる。

```bash
ANTHROPIC_BASE_URL=http://127.0.0.1:8787 \
ENABLE_TOOL_SEARCH=true \
CLAUDE_CODE_ENABLE_FINE_GRAINED_TOOL_STREAMING=1 \
claude
```

あとの2つは、`ANTHROPIC_BASE_URL`を見たClaude Codeが自分で切る機能を戻すためのもの。どちらもsitka経由で動く。

3つともシェルのprofileや`settings.json`の`env`には置かない。置くと全セッションに効いてRemote Controlが死ぬ。sitkaを使いたいセッションだけで指定する。

`CLAUDE_CODE_ATTRIBUTION_HEADER=0`は設定しない。attributionブロックはsitkaが外部プロバイダ向けの変換時に落とすので指定する必要がなく、クライアント側で切るとauto modeのclassifierがAPIに拒否されうる。`ANTHROPIC_DEFAULT_HAIKU_MODEL`にも外部モデルを割り当てない。この別名は会話タイトル生成などのバックグラウンド処理にも使われるので、意図しないところで外部プロバイダに課金が発生する。

サブエージェントで外部モデルを使うには、`sitka models`でモデルIDを確認して、frontmatterに書く。

```markdown
---
name: reviewer
description: 別モデルの視点でコードレビューする
model: openai-gpt-5.2
---

変更差分をレビューして、バグと設計上の問題を指摘してください。
```

`ANTHROPIC_BASE_URL`が設定されている間、Claude CodeはモデルIDを検証せずそのまま送るので、このサブエージェントのリクエストだけが外部プロバイダへ流れる。メインの会話はAnthropicのままになる。

## できないこと

- **Remote Controlが使えない。** `ANTHROPIC_BASE_URL`が非Anthropicホストを指している間、Claude Codeがこれを無効化する（v2.1.196以降）。復帰用の変数はなく、変数を外してセッションを開き直すしかない。
- **`/model`ピッカーに外部モデルが出ない。** ゲートウェイのモデル検出は既定で無効で、有効にしてもIDに`claude`か`anthropic`を含むものしか拾わない。メインの会話で使いたい場合は`ANTHROPIC_CUSTOM_MODEL_OPTION=openai-gpt-5.2`を設定する。
- **外部モデルのトークン数を数えられない。** `/v1/messages/count_tokens`は404を返す。OpenAI互換APIに正確に数える手段がなく、推定でごまかすことはしない。Claude Codeは推論エンドポイント経由の計測に切り替わる。
- **外部モデルに`thinking`ブロックを渡せない。** 変換時に落とす。
- **外部モデルに画像入りの`tool_result`を渡せない。** 400を返す。OpenAIの`tool`ロールはテキストしか運べないため、スクリーンショットを読ませるサブエージェントは外部モデルでは動かない。
- **外部モデルのコンテキスト長をClaude Codeが知らない。** 知らないモデルIDには独自の値を仮定するので、実際と違う場合は`CLAUDE_CODE_MAX_CONTEXT_TOKENS`で宣言する。
