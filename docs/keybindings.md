# Keybindings

Press `?` in micko for the same list on screen. Keys are case-sensitive:
`T` is shift+t.

| View | Keys |
| --- | --- |
| Navigation | `j`/`k` or arrows; `pgup`/`pgdn`; `gg`/`G` or `home`/`end` |
| Workflow list | `enter` open, `T` timeline, `X` explain, `E` events, `l` logs, `/` filter, `s` sort, `p` phase, `r` refresh, `n` namespace, `0` all namespaces, `space` mark, `W` watch for notifications, `a` actions, `w` wide columns, `esc` clear marks then filter |
| Cron workflows, templates, archive | `enter` its workflows, `i` info panel, `v` hide or reveal values, `/` search, `s` sort, `n` namespace, `0` all namespaces, `f` manifest |
| Detail | `tab`/`shift+tab` switch Summary, Nodes, Timeline, Explain, Events and Resource; `1`–`9` jump to a section; `T`, `X`, `E`, `C` jump (Compare is `7`); `r` refresh; `a` actions; `v` reveal values |
| Nodes and Timeline | `enter`/`l` logs, `space` fold, `left`/`right` fold/unfold, `i` node info, `/` find, `n`/`N` matches, `h` skipped nodes, `s` sort, `p` phase |
| Explain | `y` copy the report, `l` open failing pod's log, `n`/`N` pick failure |
| Compare | `C` or `:compare` open, `y` copy, `v` values, `r` search again, `esc` previous section |
| Events | `s` warnings first, `/` filter, `y` copy, `r` restart the stream |
| Logs | `t` follow, `G` newest, `space` pause, `c` container, `/` search, `n`/`N` matches, `&` only matching, `w` wrap, `L` labels, `ctrl+t` server timestamps, `\|` pipe |
| Actions | `u` resume, `z` suspend, `r` retry, `b` resubmit, `s` stop, `t` terminate, `d` delete; `D` finishes a delete |
| Anywhere | `:` palette, `P` profile, `f` full screen, `y` copy, `o` open in Argo UI, `?` help, `esc` back, `q` quit outside text entry, `ctrl+c` quit |

## Palette commands

`:` opens the command palette on every screen. `tab` completes, `enter` runs,
`ctrl+p`/`ctrl+n` step through earlier commands, and after `ns` or `profile` a
space completes the name.

| Command | Does |
| --- | --- |
| `workflows`, `wf` | Show the workflow list |
| `cronworkflows`, `cwf`, `cron` | Show the cron workflow list |
| `workflowtemplates`, `wftmpl`, `tmpl` | Show the workflow template list |
| `clusterworkflowtemplates`, `cwftmpl` | Show the cluster workflow template list |
| `archived`, `aw` | Show the archived workflow list |
| `compare` | Compare an open or selected workflow with the last eligible success |
| `ns [namespace]` | Switch namespace; with no name, open the namespace picker |
| `all` | Toggle the all-namespaces view |
| `profile [name]`, `ctx [name]` | Switch profile; with no name, open the profile picker |
| `mascot` | Move Mićko: perch, floor, off |
| `help` | Show every key |
| `quit`, `q` | Quit |

## Filter queries

`/` on a list filters it as you type. Terms separated by spaces must all
match; see [Filtering](usage.md#filtering) for the full query language.

With no profiles, the picker shows `micko --setup` and the config path.
Quit the picker and run setup in the terminal; use `--setup --config PATH`
for a custom file, then `micko --check --profile NAME` to check read access.
