{
  lib,
  stdenv,
  buildGoModule,
  src,
  revision ? "unknown",
  makeWrapper,
  runtimeShell,
  kubectl,
  xdg-utils,
  versionCheckHook,
}:

buildGoModule (finalAttrs: {
  pname = "micko";
  version = builtins.head (
    builtins.match ''.*const Version = "([^"]+)".*'' (
      builtins.readFile (src + "/internal/buildinfo/buildinfo.go")
    )
  );

  inherit src;

  vendorHash = "sha256-Vie8cP+GurJevwwdG1OXPHCeYC6I1CbGhtm75broJwE=";

  subPackages = [ "cmd/micko" ];

  ldflags = [
    "-s"
    "-w"
    "-X github.com/ficaa1/micko/internal/buildinfo.Commit=${revision}"
  ];

  postPatch = ''
    substituteInPlace internal/app/pipe.go \
      --replace-fail '"/bin/sh"' '"${runtimeShell}"'
  '';

  nativeBuildInputs = [ makeWrapper ];

  checkPhase = ''
    runHook preCheck
    go test ./...
    runHook postCheck
  '';

  postInstall = ''
    wrapProgram $out/bin/micko \
      --suffix PATH : ${
        lib.makeBinPath ([ kubectl ] ++ lib.optionals stdenv.hostPlatform.isLinux [ xdg-utils ])
      }
  '';

  nativeInstallCheckInputs = [ versionCheckHook ];
  versionCheckProgramArg = "--version";
  doInstallCheck = true;

  meta = {
    description = "Keyboard-first terminal UI for Argo Workflows";
    homepage = "https://github.com/ficaa1/micko";
    changelog = "https://github.com/ficaa1/micko/blob/v${finalAttrs.version}/CHANGELOG.md";
    license = lib.licenses.gpl3Only;
    mainProgram = "micko";
  };
})
