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

Every flag can also be set with a `SEABIRD_` environment variable, such as
`SEABIRD_LOG_LEVEL` for `--log-level`.
