# Trying Aux

Thanks for taking a look. This is a pre-1.0 tool that has never been used by
anyone who did not write it, which is the entire reason you are reading this.
What would help most is that you use it the way you normally work and tell us
where it surprised you.

## Before you start: what Aux does to your machine

Read this part properly. It is short, and two items are the sort of thing that
should not be a surprise.

- **Aux runs commands and edits files in the repository you point it at.** In
  interactive mode it asks permission first, per command, and you can deny.
- **`aux -p "..."` (non-interactive) approves everything automatically.** There
  is no prompt. Do not run it against a repository you care about.
- **Aux creates a `.aux/` directory in your working directory.** It holds a
  SQLite database with your full session transcript: your prompts, the model's
  replies, and the output of every tool it ran. Aux writes a `.gitignore` there
  so it will not be committed, but be aware the content is on disk in your repo.
- **Your prompts and the file contents Aux decides are relevant are sent to
  whichever model provider you configure.** Nothing else leaves the machine.
  There is no Aux service to sign up for and no telemetry.
- **Aux does start a local web server**, on by default: a dashboard bound to
  `127.0.0.1` on a random port, showing the session as it runs. It listens on
  loopback only and every data route requires a random token carried in the URL,
  so it is not reachable from another machine — but it is a listening socket you
  did not ask for, and anything running as you on your own machine could reach
  it. Turn it off with `dashboard.enabled: false` in your config.
- **That dashboard URL, token included, is written into the session
  transcript** in `.aux/`, because Aux tells you the address in its opening
  message. Worth knowing before you share a transcript or a panic log.
- Aux reads files outside the project only with your approval.

Use a repository you would not mind an agent making a mess of. A scratch clone
is ideal.

## Install

Requires Go 1.24+.

```
git clone https://github.com/kaiau00/aux-cli && cd aux-cli && go build -o aux .
```

Set a key for one provider:

```
export ANTHROPIC_API_KEY=...
```

`OPENAI_API_KEY`, `GEMINI_API_KEY`, `GROQ_API_KEY`, `AZURE_OPENAI_API_KEY` and
`OPENROUTER_API_KEY` also work, as do AWS or Google Cloud credentials for
Bedrock and Vertex AI. Run `./aux` in a repository to start.

Supported on macOS and Linux. Windows is not built or tested.

## What would be most useful to try

Please do not work from a script. The value here is that you do not know where
the sharp edges are. That said, if you want somewhere to start:

- A change that spans several files, so context handling gets exercised.
- Something in a language other than Python or Go. The only measurements so far
  are on a Python repository, so anything else is genuinely unknown ground.
- A long session. Cost accounting, context paging and history growth all only
  misbehave after a while.
- Deny a permission prompt partway through and see whether what follows makes
  sense.
- Interrupt it mid-turn.

## What to report

Anything that surprised you is worth reporting, including "I could not tell
what it was doing". Specifically valuable:

- **Anything Aux told you that was not true** — a wrong cost, a task reported
  complete that was not, a file it claimed to have changed. This class of bug
  has been the worst one repeatedly and is the hardest to find from the inside.
- **Anywhere it did something you did not expect to be allowed.**
- **Anywhere it stopped and you could not tell why.**
- Crashes. A crash writes `.aux/aux-panic-*.log`; that file is the useful part.
  It can contain source lines and prompt text, so check it before sharing.
- Anything about the first five minutes. First-run experience is the least
  tested part of this.

Rough notes in whatever form suits you are fine. Do not spend time writing them
up properly.

## Known gaps, so you do not spend time on them

- Test coverage is around 30%, well under the 80% this project asks for.
- The performance comparison against other harnesses rests on one Python
  repository and five tasks. It does not generalise yet.
- Demand paging (`--paging`) defaults to off because nothing has shown it
  lossless.
- User-defined shell hooks do not exist and are not planned.
