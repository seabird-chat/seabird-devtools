# seabird-devtools

`seabird-dev` is a tool for testing seabird components against a local
seabird-core. It can act as a fake chat backend, for testing plugins and core,
or as a plugin with access to every core RPC, for testing chat backends.

## Running

```sh
nix develop
just run --help
```

`just test` runs the tests, `just check` adds vet and the race detector, and
`just fmt` formats everything.

## Using it from another repo

Add this repo as a flake input and include the package in the devshell:

```nix
inputs.seabird-devtools.url = "github:seabird-chat/seabird-devtools";

devShells.default = pkgs.mkShell {
  packages = [
    inputs.seabird-devtools.packages.${system}.default
  ];
};
```

## Configuring

Every flag can also be set with a `SEABIRD_` environment variable:

- `SEABIRD_HOST` (optional, defaults to `http://127.0.0.1:11235`) - the
  seabird-core URL. Use `https://` for a TLS connection.
- `SEABIRD_TOKEN` (required) - an auth token from `seabird-core token create`.
- `SEABIRD_JSON` (optional) - set to `true` to print responses and events as
  protojson instead of text.
- `SEABIRD_TIMEOUT` (optional) - a duration such as `30s`. If the command is
  still running after it, it exits with an error. This bounds scripted runs,
  including `watch` and prompts.

## Plugin

`seabird-dev plugin` connects as a plugin. With no arguments it starts a prompt
and prints every event core sends while you type:

```text
plugin> backends
irc.libera type=irc
plugin> send irc://libera/%23chan hello there
ok
send_message irc://libera/%23chan from devtest: hello there
```

There is a command for every core RPC.

Other ways to run it:

- `seabird-dev plugin <command> [args...]` runs one command and exits, such as
  `seabird-dev plugin channels irc.libera`.
- `seabird-dev plugin watch` prints events until interrupted. With
  `--count N`, it exits after N events.
- Piping commands into `seabird-dev plugin` runs them in order.

Core only delivers command events to plugins which register the command. Pass
`--command NAME` or `--command NAME:SHORT_HELP` once per command.

## Backend

`seabird-dev backend` connects as a fake chat backend, for testing plugins and
core without a real chat network. Its prompt sends events as fake users, and it
prints and answers every request core sends:

```text
backend> join #general
backend> cmd #general alice weather 98101
send_message #general: It's sunny in 98101
```

Channel and user IDs are the names typed at the prompt, such as `#general` and
`alice`. Core namespaces them for plugins, so with the default `--type dev` and
`--id local`, plugins see `#general` as `dev://local/%23general`. Channels must
be joined before sending to them, either with `join` or with `--join #general`
(repeatable) at startup.

Requests are answered the way a working backend would: joins, leaves and topic
changes update the channel list, and everything else succeeds. `respond`
changes that, to test how plugins and core handle a misbehaving backend:

- `respond fail [reason]` fails every request.
- `respond ignore` never answers, so requests time out in core.
- `respond delay 5s` answers normally after a delay.
- `respond ok` goes back to normal.

Piping commands into `seabird-dev backend` runs them in order, then disconnects
once core has received everything. Use `sleep` to wait for replies before the
input ends:

```sh
printf 'join #general\ncmd #general alice ping\nsleep 2s\n' | seabird-dev backend
```

## Prompts

`help` lists the commands at either prompt, and `sleep <duration>` waits.
Arguments are split on whitespace, except the last argument of commands like
`send` and `msg`, which takes the rest of the line. History is saved in the
user cache directory, under `seabird-dev/`.

Line editing is used only when stdin and stdout are a terminal and `TERM` isn't
`dumb`. Otherwise commands are read one line at a time, which suits pipes and
terminals that don't answer cursor position queries.

### Formatting

Commands which send messages parse their text as Discord-flavored markdown and
send it as message blocks: `send`, `pm`, `action` and `paction` in the plugin,
and `msg`, `action`, `mention`, `pm` and `paction` in the backend. Pass
`--plain` to send text as-is.

| Markdown                         | Block                   |
| -------------------------------- | ----------------------- |
| `*italics*` or `_italics_`       | italics                 |
| `**bold**`                       | bold                    |
| `__underline__`                  | underline               |
| `~~strikethrough~~`              | strikethrough           |
| `\|\|spoiler\|\|`                | spoiler                 |
| `` `code` ``                     | inline code             |
| ```` ```lang\ncode\n``` ````     | fenced code             |
| `[text](url)`, `<url>`, bare URL | link                    |
| `# heading`                      | heading                 |
| `> quote`                        | blockquote              |
| `- item`                         | list                    |
| `<t:1700000000>`                 | timestamp (unix second) |

A prompt line can't contain a newline, so a literal `\n` is treated as one,
for example `send irc://libera/%23chan - one\n- two`. Markdown without a
matching block, such as images, is rejected.

When stdout is a terminal, incoming messages are shown with their formatting,
in plugin events and backend requests alike,
using terminal styles and colors much like an IRC client. Spoilers are shown in
reverse video and timestamps in local time. Set `NO_COLOR` to turn this off.
Otherwise, and with `--json`, messages are shown as plain text.
