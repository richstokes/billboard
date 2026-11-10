# billboard

Streams source code from your projects in a terminal, with a few different effects and themes. Built to sit in the background of an OBS scene or video.

## Install

```sh
go install github.com/richstokes/billboard@latest
```

Or clone and `go build -o billboard .`

## Use

```sh
billboard ~/code/myproject                 # pick file types interactively
billboard ~/code/a ~/code/b --ext go,ts    # skip the picker
billboard . --effect matrix --theme matrix
billboard . --effect fade,scroll --brightness 0.5 --speed 0.7
```

It walks the dirs recursively and skips the usual junk (dotdirs, `node_modules`, `vendor`, `dist`, lockfiles, binaries, anything over 512KB). Add more with `--exclude`.

Since this is meant for streams, anything that might hold a secret is never shown, even if you pass it with `--ext`:

- Dotfiles and dot-dirs (`.env`, `.ssh`, `.aws`, ...), plus dirs named `secrets`, `credentials`, `certs` or `keys`
- Env files, keys and certs, keystores, Terraform state and tfvars, `id_rsa` and friends, `.netrc`/`.npmrc`/`.pypirc` (see `secrets.go`)
- Config files with names like `secrets.yaml` or `service-account.json`
- Any file whose contents look like they include a secret: private keys, AWS/GitHub/GitLab/Slack/Stripe/Google/OpenAI/Anthropic tokens, passwords in connection URLs, or `password = "..."` style assignments

It's pattern matching, so it won't catch everything. Worth running it over a project once before going live. After using the picker it prints the equivalent `--ext` command on exit, so you can drop that into a script.

`billboard -h` lists every flag, effect and theme.

### Remote repos

```sh
billboard --repo richstokes/billboard            # GitHub shorthand
billboard --repo git@gitlab.com:me/thing.git src  # any git URL; dirs are paths inside the repo
billboard --repo me/thing --watch                 # show new commits as they land
```

Clones into the system temp dir (`$TMPDIR/billboard/repos/`) and reuses it if you restart, so there's no wait mid-stream. The OS cleans it up on reboot (macOS also clears it after a few days unused), and `--fresh` forces a clean re-clone. It's a shallow clone of the last 200 commits, so big repos stay quick and diff replay still has history. Private repos work with whatever git credentials you already have.

With `--watch` it pulls every 10 seconds (`--repo-interval` to change it) and plays whatever files the new commits touched. Diff replay shows that new commit for each file.

### Worktrees

`--include-worktrees` also scans every linked git worktree of any repo under the dirs, even when the worktree lives outside them (`../proj-feature`) or somewhere hidden like `.claude/worktrees/`. Files from a worktree are labelled `repo@branch/...` in the header. Works with `--watch` too, and new worktrees get picked up within ~15 seconds.

### Watch mode

```sh
billboard --watch --ext go,ts ~/code/myproject
```

Starts on a blank "waiting" screen and only shows files as they're added or saved, so the background follows what you're actually working on. If several files change at once they queue up and play in the order they changed (the header shows how many are waiting). Once the queue is empty it stays on the last file until something else changes. It rescans once a second, so there's up to a second of lag after a save.

### Effects

| Effect | What it does |
| --- | --- |
| `typewriter` | Types the code live with a blinking cursor, natural pauses and the odd typo + backspace |
| `scroll` | Continuous credit-roll scroll, flows from one file straight into the next |
| `matrix` | Digital rain (katakana + characters from the file) that reveals the code where it falls |
| `decrypt` | Scrambled glyphs that resolve into code, line by line |
| `fade` | Lines gently fade and drift in. The subtle one |
| `hack` | Code flies in from all over the screen, then a fake "BYPASSING FIREWALL" progress box and a flashing "ACCESS GRANTED" |
| `starfield` | Characters from the file fly past at warp speed while real lines land one at a time |
| `boot` | CRT warm-up and a fake boot sequence before the file is listed. Themed for `c64` (BASIC screen + border), `dreamcast` (spinning swirl) and `matrix` ("Wake up, Neo...") |
| `diff` | Replays a real git commit that touched the file: removed lines go red and collapse, new lines type in green. The header shows the commit. Files with no history fall back to another effect |

Default is `--effect all`, which picks one at random per file. Diff replay also runs the secret checks on old versions of the file, since a key that was deleted later still lives in history.

### Themes

`dracula`, `tokyonight`, `monokai`, `nord`, `gruvbox`, `synthwave`, `github-dark`, `c64`, `dreamcast`, plus single-hue CRT themes `matrix`, `amber`, `ice`, `mono`.

### Keys

| Key | |
| --- | --- |
| `n` / `→` | next file |
| `space` | pause |
| `e` | cycle effect (mix → each one) |
| `t` / `T` | cycle theme |
| `+` / `-` | speed |
| `b` / `B` | dimmer / brighter |
| `l` / `h` | toggle line numbers / header |
| `?` | key help |
| `q` | quit |

## OBS tips

- Use a Window Capture (macOS Screen Capture → Window) on the terminal, and hide the terminal's own chrome/scrollbar if you can
- `--brightness 0.4`-`0.6` keeps it from fighting with whatever's in front of it. You can also just tweak it live with `b`/`B`
- `--transparent` skips painting a background so the terminal's own background/opacity shows through
- Bigger font + fewer columns reads better on stream than a wall of tiny text
- `--header=false --line-numbers=false` for a cleaner look
