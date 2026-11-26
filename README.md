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

`help` lists every command, and there is one per core RPC. Arguments are split
on whitespace, except the last argument of commands like `send`, which takes
the rest of the line.

Other ways to run it:

- `seabird-dev plugin <command> [args...]` runs one command and exits, such as
  `seabird-dev plugin channels irc.libera`.
- `seabird-dev plugin watch` prints events until interrupted.
- Piping commands into `seabird-dev plugin` runs them in order.

Core only delivers command events to plugins which register the command. Pass
`--command NAME` or `--command NAME:SHORT_HELP` once per command.

Prompt history is saved in the user cache directory, under `seabird-dev/`.
