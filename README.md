# Code Navigation MCP

A local stdio MCP server that runs Codecore and Codegos as subprocesses. MCP clients send structured arguments and receive the CLIs' compact text with line anchors. The adapter uses the official Go MCP SDK.

| MCP tool | Arguments | Equivalent CLI |
| --- | --- | --- |
| `codecore_outline` | `path`, optional `depth` from 0 to 4 (default 0) | `codecore --depth N -- PATH` |
| `codegos_outline` | `path` | `codegos PATH --plain` |
| `codegos_expand` | `path`, `line` at the start of a heading | `codegos PATH go LINE --plain` |
| `codegos_read` | `path`, inclusive `start` and `end`, both 1-based | `codegos PATH show START-END --plain` |

Codecore outlines supported files in one directory level. Use a single file for deeper inspection. Codecore's text format gives line anchors and nesting. Codegos outlines give comment and block spans. A file without standalone comments yields just the compact file header. Codegos comment navigation supports Go, Python, and PowerShell. For other text files, use `codegos_read` for exact line ranges. Expansion preserves Codegos's own child listings and short leaf previews. Explicit reads preserve source indentation, blank lines, and line numbers.

The inspected Codecore source at `8856f93` and the matching installed binary have no ranked search command. Ranked search and a search-result limit are therefore unavailable in this version. [Codecore](https://github.com/black-ether/codecore) · [Codegos](https://github.com/black-ether/codegos)

## Configuration and bounds

| Flag | Default | Effect |
| --- | --- | --- |
| `--workspace PATH` | Required | Source paths are relative to this root. Absolute paths inside the resolved root also work. |
| `--tools FAMILY` | `both` | Choose `codecore`, `codegos`, or `both`. Only enabled families need a binary. |
| `--codecore PATH` | `codecore` on PATH | Use an explicit executable path when the client has a different PATH. |
| `--codegos PATH` | `codegos` on PATH | Use an explicit executable path when the client has a different PATH. |
| `--max-output-bytes N` | `8192` | UTF-8 tool text budget in bytes, minimum 256. MCP framing is additional. |
| `--max-read-lines N` | `100` | Maximum inclusive size of one explicit source read. |
| `--timeout DURATION` | `5s` | Per-call deadline. Use Go durations such as `2s` or `500ms`. |

The adapter captures each CLI stream within the configured byte budget and returns CLI text unchanged when it fits and contains valid UTF-8. It appends successful CLI warnings, supplies a short message for empty stdout, and returns CLI failures as MCP tool errors. Oversized text retains its leading anchors, may end in a partial row, and includes an incomplete-result notice with a narrower follow-up. A timeout returns a tool error. Neither CLI's JSON mode nor Codegos's `--full` is exposed.

Executable and workspace settings belong to the local operator. Select the intended repository as the workspace. The adapter checks lexical and resolved paths, rejects symlinks and junctions that resolve outside the workspace, and rejects indirect or special file entries during directory outlining. It runs the configured binaries directly with argument arrays. The CLIs reopen files by path after validation, so the workspace and its parent directories must be trusted against concurrent path replacement. The deadline covers subprocess execution and is checked during directory enumeration. An OS filesystem call that blocks can still delay path validation.

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

## Transfer to Muse's Ubuntu VM

1. From this project directory on Windows, replace `USER@HOST` with the SSH login for the VM and transfer the Linux adapter.

   ```powershell
   ssh USER@HOST 'mkdir -p ~/code-navigation-bin'
   scp dist/code-navigation-mcp-linux-amd64 USER@HOST:code-navigation-bin/code-navigation-mcp
   ```

2. On the VM, prepare the three Linux executables. Reuse existing Codecore and Codegos binaries if they match the inspected syntax. If you need source checkouts, clone Codecore at `8856f93` and Codegos at `bc789ba`. Use Go 1.27 or newer to build both and adjust the checkout locations below. [Codecore](https://github.com/black-ether/codecore) · [Codegos](https://github.com/black-ether/codegos)

   ```bash
   chmod +x "$HOME/code-navigation-bin/code-navigation-mcp"
   (cd "$HOME/src/codecore" && CGO_ENABLED=0 go build -trimpath -o "$HOME/code-navigation-bin/codecore" .)
   (cd "$HOME/src/Codegos" && CGO_ENABLED=0 go build -trimpath -o "$HOME/code-navigation-bin/codegos" .)
   "$HOME/code-navigation-bin/code-navigation-mcp" --help
   "$HOME/code-navigation-bin/codecore" --help
   "$HOME/code-navigation-bin/codegos" --help
   ```

3. Open `hermes -p PROFILE config edit`, where `PROFILE` is Muse's experiment profile. Use `default` if Muse runs the default profile. Merge the following into the existing `mcp_servers` map. Replace `/home/muse` with the actual absolute home and select the target workspace.

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

4. Run `hermes -p PROFILE mcp list` to list configured servers and `hermes -p PROFILE mcp test code_navigation` to connect and discover the four tools without a model call. Start a fresh Hermes session for the profile or use `/reload-mcp` in a running session.

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
