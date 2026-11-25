# Code Navigation MCP

A local stdio MCP server that runs Codecore and Codegos as subprocesses. MCP clients send structured arguments and receive the CLIs' compact text with line anchors. The adapter uses the official Go MCP SDK.

| MCP tool | Arguments | Equivalent CLI |
| --- | --- | --- |
| `codecore_outline` | `path`, optional `depth` from 0 to 4 | `codecore [--depth N] -- PATH` |
| `codecore_search` | `path`, `query`, optional `limit`, `def_only`, `per_file`, `word`, `no_tests` | `codecore --ask QUERY --top N [--def-only] [--per-file] [--word] [--no-tests] -- PATH` |
| `codegos_outline` | `path` | `codegos PATH --plain` |
| `codegos_expand` | `path`, `line` at the start of a heading | `codegos PATH go LINE --plain` |
| `codegos_read` | `path`, inclusive `start` and `end`, both 1-based | `codegos PATH show START-END --plain` |

Codecore outlines and searches supported files recursively when given a directory. Outlines give line anchors and nesting. Ranked search uses the CLI's ranking and preserves file paths, ranges, scores, and enclosing blocks. Prefer a symbol or short topic for the query. The default hit count is 5, or the configured search cap if lower. All filters default to off. Set `def_only` for definitions, `per_file` for the best hit per file, `word` for whole-word matching, or `no_tests` to exclude test code. Both Codecore tools pass canonical workspace-relative paths to the CLI, so the workspace's parent directories cannot affect `no_tests`.

Codegos outlines give comment and block spans. A file without standalone comments yields just the compact file header. Codegos comment navigation supports Go, Python, and PowerShell. For other text files, use `codegos_read` for exact line ranges. Expansion preserves Codegos's child listings and short leaf previews. Explicit reads preserve source indentation, blank lines, and line numbers.

## CLI compatibility

- **Codecore** — Use experiment revision `26b8bdecaba0531c5b84746761bab214bb457a64`. This build includes recursive traversal, ranked search, and matches on attached comments and Python docstrings. Omit `depth` to use the CLI's default per language (2 for HTML/XML, 0 for most others). An explicit `0` stays top-level. The original adapter's Codecore revision `8856f93` fails the startup check. Token-pattern search, filename-only output, and counts are CLI-only options. [Codecore revision](https://github.com/black-ether/codecore/tree/26b8bdecaba0531c5b84746761bab214bb457a64)
- **Codegos** — Use `bc789ba8aa4dde67067acbabb19f2da0b52741ef` for a reproducible build. The MCP contract requires compact `--plain` outlines, progressive expansion, and numbered `show` reads. Unrestricted `--full` remains a CLI-only option. [Codegos revision](https://github.com/black-ether/codegos/tree/bc789ba8aa4dde67067acbabb19f2da0b52741ef)

Startup checks the enabled executables against a temporary fixture. Codecore must demonstrate recursive outlines and ranked search with comment matches and the supported flags. Codegos must demonstrate all three formats of navigation. A failed check identifies the executable and exits before serving MCP. For benchmark reproducibility, check the binaries with `go version -m` and pin their build revisions separately. A successful probe checks the CLI contract and permits other compatible builds.

Windows stdio parity was checked against a clean build of Codecore at the revision above. Codegos parity was checked with the clean `bc789ba` build, the locally installed `bc789ba+dirty` build, and the older checkout binary at `3ee52ca3e24b25462e149f4d4dce5c694d1f546a+dirty`. Both older or locally modified Codegos builds matched the tested behavior. Use the clean pinned builds for new runs.

## Configuration and bounds

| Flag | Default | Effect |
| --- | --- | --- |
| `--workspace PATH` | Required | Source paths are relative to this root. Absolute paths inside the resolved root also work. |
| `--tools FAMILY` | `both` | Choose `codecore`, `codegos`, or `both`. Only enabled families need a binary. |
| `--codecore PATH` | `codecore` on PATH | Use an explicit executable path when the client has a different PATH. |
| `--codegos PATH` | `codegos` on PATH | Use an explicit executable path when the client has a different PATH. |
| `--max-output-bytes N` | `8192` | UTF-8 tool text budget in bytes, minimum 256. MCP framing is additional. |
| `--max-read-lines N` | `100` | Maximum inclusive size of one explicit source read. |
| `--max-search-results N` | `50` | Maximum value of the search tool's `limit`. Codecore still returns fewer hits when applicable. |
| `--timeout DURATION` | `5s` | Per-call deadline. Use Go durations such as `2s` or `500ms`. |

The adapter captures each CLI stream within the configured byte budget and returns CLI text unchanged when it fits and contains valid UTF-8. It appends successful CLI warnings, supplies a short message for empty stdout, and returns CLI failures as MCP tool errors. Oversized text retains leading complete rows and adds an incomplete-result notice with a narrower follow-up. If the first row alone is too long, its leading text is retained. A timeout returns a tool error.

Executable and workspace settings belong to the local operator. Select the intended repository as the workspace. For individual paths, the adapter checks the resolved location against the workspace. Before a Codecore directory outline or search, it checks all descendants and rejects symlinks, junctions, and special entries, including in directories the CLI would skip. Use a narrower path if that check rejects a repository-wide call. The CLIs reopen paths after validation, so the workspace and its parent directories must be trusted against concurrent path replacement. The deadline covers subprocess execution and is checked during directory enumeration. A blocking filesystem call can delay validation.

## Build and test

Requires Go 1.26 or newer for the adapter. The bundled builds target Windows amd64 and Linux amd64. Each host also needs its native Codecore and Codegos binaries for the enabled families.

PowerShell, from this directory:

```powershell
go test ./...
go vet ./...
New-Item -Force -ItemType Directory dist | Out-Null
$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
go build -trimpath -o dist/code-navigation-mcp-windows-amd64.exe .
$env:GOOS = 'linux'
go build -trimpath -o dist/code-navigation-mcp-linux-amd64 .
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
```

Ubuntu amd64, from this directory:

```bash
go test ./...
go vet ./...
mkdir -p dist
CGO_ENABLED=0 go build -trimpath -o dist/code-navigation-mcp-linux-amd64 .
```

Set these environment variables to absolute native executable paths and run `go test -run TestRealCLIsOverStdio -v` to compare the compiled adapter's MCP text with direct CLI output. The test initializes the adapter over stdio and makes tool calls without a model. The regular suite also checks tool discovery and all three exposure modes.

- `CODE_NAV_MCP_BIN` for the compiled adapter
- `CODE_NAV_CORE_BIN` for Codecore
- `CODE_NAV_GOS_BIN` for Codegos

## Codex on Windows

Add this to a trusted project's `.codex/config.toml` or to `~/.codex/config.toml`. Replace the paths and choose the workspace you want to expose. TOML single-quoted strings keep Windows paths literal.

```toml
[mcp_servers.code_navigation]
command = 'C:\Agent Tools\code-navigation-mcp-windows-amd64.exe'
args = ['--workspace', 'C:\Projects\target repo', '--codecore', 'C:\Agent Tools\codecore.exe', '--codegos', 'C:\Agent Tools\codegos.exe', '--tools', 'both']
```

`codex mcp list` shows registered servers. Start a Codex session and use `/mcp` to check the live connection and tool list. To disable the server, set `enabled = false` in its table. Remove a manually added table from its config, or use `codex mcp remove code_navigation` for an entry registered using the CLI. [Codex MCP guide](https://developers.openai.com/codex/mcp)

## Hermes on native Windows

Open the intended profile's configuration with `hermes -p PROFILE config edit`. Merge this example under the existing `mcp_servers` key. For the default native installation, the configuration is under `%LOCALAPPDATA%\hermes`.

```yaml
mcp_servers:
  code_navigation:
    command: 'C:\Agent Tools\code-navigation-mcp-windows-amd64.exe'
    args: ['--workspace', 'C:\Projects\target repo', '--codecore', 'C:\Agent Tools\codecore.exe', '--codegos', 'C:\Agent Tools\codegos.exe', '--tools', 'both']
```

Use the Linux example within a WSL-based Hermes installation. [Native Windows guide](https://hermes-agent.nousresearch.com/docs/user-guide/windows-native) · [Hermes MCP guide](https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp)

## Install on Muse's Ubuntu VM

1. On the VM, clone over outbound HTTPS and build with Go 1.27 or newer. The first adapter build downloads Go modules.

   ```bash
   mkdir -p "$HOME/src" "$HOME/code-navigation-bin"
   git clone https://github.com/black-ether/codecore.git "$HOME/src/codecore"
   git clone https://github.com/black-ether/codegos.git "$HOME/src/Codegos"
   git clone https://github.com/black-ether/code-navigation-mcp.git "$HOME/src/code-navigation-mcp"
   git -C "$HOME/src/codecore" switch --detach 26b8bdecaba0531c5b84746761bab214bb457a64
   git -C "$HOME/src/Codegos" switch --detach bc789ba8aa4dde67067acbabb19f2da0b52741ef
   (cd "$HOME/src/codecore" && CGO_ENABLED=0 go build -trimpath -o "$HOME/code-navigation-bin/codecore" .)
   (cd "$HOME/src/Codegos" && CGO_ENABLED=0 go build -trimpath -o "$HOME/code-navigation-bin/codegos" .)
   (cd "$HOME/src/code-navigation-mcp" && CGO_ENABLED=0 go build -trimpath -o "$HOME/code-navigation-bin/code-navigation-mcp" .)
   go version -m "$HOME/code-navigation-bin/codecore" "$HOME/code-navigation-bin/codegos"
   (cd "$HOME/src/code-navigation-mcp" &&
     CODE_NAV_MCP_BIN="$HOME/code-navigation-bin/code-navigation-mcp" \
     CODE_NAV_CORE_BIN="$HOME/code-navigation-bin/codecore" \
     CODE_NAV_GOS_BIN="$HOME/code-navigation-bin/codegos" \
     go test -count=1 ./...)
   ```

2. Open `hermes -p PROFILE config edit`, where `PROFILE` is Muse's experiment profile. Use `default` if Muse runs the default profile. Merge the following into the existing `mcp_servers` map. Replace `/home/muse` with the actual absolute home and select the target workspace.

   ```yaml
   mcp_servers:
     code_navigation:
       command: /home/muse/code-navigation-bin/code-navigation-mcp
       args:
         - --workspace
         - /home/muse/experiment/target-repo
         - --codecore
         - /home/muse/code-navigation-bin/codecore
         - --codegos
         - /home/muse/code-navigation-bin/codegos
         - --tools
         - both
   ```

3. Run `hermes -p PROFILE mcp list` to list configured servers and `hermes -p PROFILE mcp test code_navigation` to connect and discover the five tools without a model call. Start a fresh Hermes session for the profile or use `/reload-mcp` in a running session.

For a Codecore-only or Codegos-only condition, change the `--tools` value to `codecore` or `codegos` in that profile. Hermes also supports a per-server `tools.include` list of MCP tool names. Use `enabled: false` to disable the server, or remove the entry. These commands apply to Windows and Linux. [Hermes MCP configuration reference](https://hermes-agent.nousresearch.com/docs/reference/mcp-config-reference) · [Hermes profiles](https://hermes-agent.nousresearch.com/docs/user-guide/profiles)

## Example requests and results

Set the workspace to the Codegos checkout to use its `examples/sample.ps1` fixture. After MCP initialization, send one JSON-RPC request per line. Each successful response has one text item in `result.content`. The blocks below show the text of each response.

```json
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"codecore_outline","arguments":{"path":"examples/sample.ps1"}}}
```

```text
[L:3]  Import-Module SomeModule
[L:7]  $config = Get-Content config.json
[L:9]  if (!$config) { throw 'Missing config' }
[L:12] Start-Process app.exe
```

```json
{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"codecore_search","arguments":{"path":"examples/sample.ps1","query":"config","limit":2,"word":true}}}
```

```text
  1. examples/sample.ps1 [L:7] $config = Get-Content config.json
     score 4.14
  2. examples/sample.ps1 [L:9] if (!$config) { throw 'Missing config' }
     score 3.98
```

```json
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"codegos_outline","arguments":{"path":"examples/sample.ps1"}}}
```

```text
sample.ps1 (powershell, 13 lines)
1-2 | region Initialization Load required modules [block 1-4]
  5 | Configure environment (2 children) [block 5-10]
 11 | Run application [block 11-12]
 13 | endregion [block 13-13]
```

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"codegos_expand","arguments":{"path":"examples/sample.ps1","line":5}}}
```

```text
  5 | Configure environment (6 lines) [block 5-10]
  6 |   Read configuration (2 lines) [block 6-7]
  7 |         $config = Get-Content config.json
  8 |   Validate configuration (3 lines) [block 8-10]
  9 |         if (!$config) { throw 'Missing config' }
 10 |
```

```json
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"codegos_read","arguments":{"path":"examples/sample.ps1","start":8,"end":10}}}
```

```text
 8 |     # Validate configuration
 9 |     if (!$config) { throw 'Missing config' }
10 |
```

## License

Code Navigation MCP is licensed under the Apache License 2.0. Release archives include the project license and third-party notices. [License](LICENSE) · [Notice](NOTICE)
