# portwatch

Live-updating TUI showing all services listening on localhost ports. Like `lsof -iTCP` but beautiful and always-on.

## Build

```bash
go build -o portwatch .
```

## Usage

```bash
./portwatch          # TUI mode (live-updating, interactive)
./portwatch list     # one-shot list (no TUI)
```

## TUI Keys

| Key | Action |
|-----|--------|
| `j/k` or arrows | navigate |
| `o` | open in browser |
| `K` | kill process |
| `c` | copy curl command |
| `l` | label/bookmark port |
| `/` | filter |
| `g/G` | top/bottom |
| `q` | quit |

## Structure

Single-file `main.go`:
- Port scanning via `lsof -iTCP -sTCP:LISTEN`
- Full process names via `ps -p PID -c -o comm=`
- Docker container mapping via `docker ps --format`
- Labels persisted to `~/.config/portwatch/labels.json`
- Auto-refresh every 2s

## Stack

- Go + charmbracelet/bubbletea (TUI framework)
- charmbracelet/bubbles (text input)
- charmbracelet/lipgloss (styling)
- Zero external runtime deps — single binary

## Differentiators vs sonar

- Live-updating TUI (sonar is one-shot CLI)
- Bookmarks/labels that persist across sessions
- Filter/search
- Visual selection + quick actions
