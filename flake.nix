{
  description = "opqs — secure keyboard-first 1Password picker";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs { inherit system; config.allowUnfree = true; };

      daemon = pkgs.buildGoModule {
        pname = "opqs";
        version = "0.1.0";
        src = ./.;
        vendorHash = null;
        postInstall = ''
          mkdir -p $out/share/opqs
          cp -r ui $out/share/opqs/ui
        '';
        meta.mainProgram = "opqs";
      };

      client = pkgs.writeShellApplication {
        name = "opqs-client";
        runtimeInputs = [ daemon pkgs.quickshell pkgs.niri pkgs.wtype pkgs._1password-cli pkgs.procps pkgs.coreutils pkgs.util-linux ];
        text = ''
          export QML2_IMPORT_PATH="$HOME/.config/quickshell:$HOME/.local/share/qml''${QML2_IMPORT_PATH:+:$QML2_IMPORT_PATH}"
          sock="$XDG_RUNTIME_DIR/opqs.sock"
          exec 9>"$XDG_RUNTIME_DIR/opqs-launch.lock"
          flock 9

          current=$(readlink -f "${daemon}/bin/opqs")
          for pid in $(pgrep -x opqs 2>/dev/null || true); do
            exe=$(readlink -f "/proc/$pid/exe" 2>/dev/null || true)
            if [ -n "$exe" ] && [ "$exe" != "$current" ]; then
              kill "$pid" 2>/dev/null || true
              for _ in $(seq 1 30); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
            fi
          done

          alive=""
          for pid in $(pgrep -x opqs 2>/dev/null || true); do
            case "$(ps -o stat= -p "$pid" 2>/dev/null)" in Z*|"") ;; *) alive=1 ;; esac
          done
          if [ -z "$alive" ]; then
            rm -f "$sock"
            setsid nohup ${daemon}/bin/opqs >/dev/null 2>&1 </dev/null 9>&- &
          fi
          for _ in $(seq 1 50); do [ -S "$sock" ] && break; sleep 0.1; done
          [ -S "$sock" ] || exit 1

          ${daemon}/bin/opqs summon

          if ! pgrep -f "quickshell.* -p ${daemon}/share/opqs/ui" >/dev/null 2>&1; then
            setsid nohup qs -n -p "${daemon}/share/opqs/ui" >/dev/null 2>&1 </dev/null 9>&- &
          fi
        '';
      };
    in {
      packages.${system} = {
        opqs = daemon;
        opqs-client = client;
        default = client;
      };

      checks.${system}.tests = daemon.overrideAttrs (_: {
        doCheck = true;
        checkPhase = "go test ./...";
      });
    };
}
