{
  description = "filebox: browse, preview and share files on local disks";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";

  outputs =
    { self, nixpkgs }:
    let
      forSystem = system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          # bun's node_modules as a fixed-output derivation; bump webDepsHash when web/bun.lock changes
          webDeps = pkgs.stdenvNoCC.mkDerivation {
            name = "filebox-web-deps";
            src = ./web;
            nativeBuildInputs = [ pkgs.bun ];
            dontConfigure = true;
            buildPhase = ''
              export HOME=$TMPDIR
              bun install --frozen-lockfile --no-progress --ignore-scripts --no-cache
            '';
            installPhase = ''
              cp -R node_modules $out
            '';
            dontFixup = true;
            outputHashMode = "recursive";
            outputHash = "sha256-nbPZY69BIcGrsoUSK2xglOcEVzH6EU3YKdOtewCNZns=";
          };
          web = pkgs.stdenvNoCC.mkDerivation {
            name = "filebox-web";
            src = ./web;
            nativeBuildInputs = [ pkgs.bun ];
            buildPhase = ''
              export HOME=$TMPDIR
              cp -R ${webDeps} node_modules
              chmod -R u+w node_modules
              for m in index share reader; do bun node_modules/vite/bin/vite.js build --mode $m; done
            '';
            installPhase = "cp -R dist $out";
          };
        in
        pkgs.buildGo126Module {
          pname = "filebox";
          version = self.shortRev or "dirty";
          src = self;
          vendorHash = "sha256-4k70EQ+MzaXw6IgTv03b44PAU/mNzgskbtS0JP4HfU0=";
          subPackages = [ "cmd/filebox" ];
          env.CGO_ENABLED = 0;
          ldflags = [ "-s" "-w" ];
          preBuild = ''
            rm -rf web/dist
            cp -R ${web} web/dist
          '';
          meta.mainProgram = "filebox";
        };
    in
    {
      packages = nixpkgs.lib.genAttrs [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ] (s: { default = forSystem s; });
    };
}
