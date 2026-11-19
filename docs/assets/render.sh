#!/bin/sh
# Regenera los visuales de la documentacion: GIFs (VHS), diagramas (d2) y videos (HyperFrames).
#
#   sh docs/assets/render.sh [--dry-run] [todo|gifs|diagramas|videos] [nombres...]
#
# Sin nombres regenera todo el grupo; con nombres, solo esos (un GIF que cambia no
# reescribe los otros 11 en el historial de git). Todo corre en Docker: nunca baja ni
# construye una imagen por su cuenta. Si falta una, imprime el comando exacto y sale con 2.
# Hay un equivalente para Windows PowerShell en docs/assets/render.ps1.

VHS_IMAGE="nxy-docs-vhs:v0.12.1"
VHS_SIZE="~30 MB sobre ghcr.io/charmbracelet/vhs:v0.12.1, que pesa ~887 MB comprimida / ~3 GB en disco; si no la tenés, antes: docker pull ghcr.io/charmbracelet/vhs:v0.12.1"
VHS_BUILD="docker build -t nxy-docs-vhs:v0.12.1 docs/assets/tapes"
VIDEO_IMAGE="nxy-docs-video:0.9.10"
VIDEO_SIZE="unos 2 GB en disco (node, chromium, ffmpeg, fuentes y HyperFrames)"
VIDEO_BUILD="docker build -t nxy-docs-video:0.9.10 docs/assets/video"
D2_IMAGE="terrastruct/d2:v0.9.0"
D2_SIZE="~95 MB"

DRY=0
if [ "$1" = "--dry-run" ]; then
	DRY=1
	shift
fi
GROUP="${1:-todo}"
[ $# -gt 0 ] && shift
NAMES="$*"

case "$GROUP" in
todo | gifs | diagramas | videos) ;;
*)
	echo "Grupo desconocido: $GROUP. Opciones: todo, gifs, diagramas, videos." >&2
	exit 1
	;;
esac

# Raiz del repo a partir de la ubicacion del script, sin depender del cwd.
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)
cd "$ROOT" || exit 1

# Git Bash en Windows: sin esto reescribe las rutas de docker; el montaje usa la ruta de Windows.
MOUNT="$ROOT"
case "$(uname -s)" in
MINGW* | MSYS*)
	export MSYS_NO_PATHCONV=1
	MOUNT=$(pwd -W)
	;;
esac

# Listas de lo que existe en disco (un nombre por linea).
list_names() { # dir ext
	for f in "$1"/*."$2"; do
		[ -e "$f" ] || continue
		basename "$f" ".$2"
	done
}
list_videos() {
	for d in docs/assets/video/*/; do
		[ -f "${d}index.html" ] || continue
		n=$(basename "$d")
		case "$n" in out | node_modules) continue ;; esac
		echo "$n"
	done
}
TAPES=$(list_names docs/assets/tapes tape)
DIAGRAMS=$(list_names docs/assets/diagrams d2)
VIDEOS=$(list_videos)

# Valida los nombres pedidos contra lo que existe para el grupo.
case "$GROUP" in
gifs) VALID="$TAPES" ;;
diagramas) VALID="$DIAGRAMS" ;;
videos) VALID="$VIDEOS" ;;
*) VALID=$(printf '%s\n%s\n%s\n' "$TAPES" "$DIAGRAMS" "$VIDEOS") ;;
esac
for n in $NAMES; do
	if ! printf '%s\n' "$VALID" | grep -qx "$n"; then
		echo "No existe '$n' en el grupo $GROUP. Nombres validos:" >&2
		printf '%s\n' "$VALID" | grep -v '^$' | sed 's/^/  /' >&2
		exit 1
	fi
done

wanted() { # nombre -> 0 si hay que regenerarlo
	[ -z "$NAMES" ] && return 0
	for n in $NAMES; do [ "$n" = "$1" ] && return 0; done
	return 1
}

run() { # imprime el comando; lo ejecuta salvo en --dry-run
	echo "+ $*"
	[ "$DRY" = 1 ] && return 0
	"$@"
}

check_docker() {
	[ "$DRY" = 1 ] && return 0
	if ! docker info >/dev/null 2>&1; then
		echo "Docker no responde: abrí Docker Desktop (o iniciá el servicio docker) y volvé a correr este comando." >&2
		exit 2
	fi
}

need_image() { # imagen tamano comando-para-conseguirla
	[ "$DRY" = 1 ] && return 0
	if ! docker image inspect "$1" >/dev/null 2>&1; then
		echo "Falta la imagen $1 (pesa $2). Este script no la baja solo; conseguila con:" >&2
		echo "  $3" >&2
		exit 2
	fi
}

step_build() {
	echo "== Binario de nxy para Linux (lo usan los contenedores) =="
	if [ "$DRY" = 1 ]; then
		echo "+ CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o docs/assets/.build/nxy ./cmd/nxy"
		return 0
	fi
	mkdir -p docs/assets/.build
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o docs/assets/.build/nxy ./cmd/nxy || exit 1
}

step_gifs() {
	echo "== GIFs (VHS) =="
	if [ -z "$TAPES" ]; then
		echo "No hay tapes en docs/assets/tapes/*.tape: nada que grabar."
		return 0
	fi
	check_docker
	need_image "$VHS_IMAGE" "$VHS_SIZE" "$VHS_BUILD"
	mkdir -p docs/assets/gifs
	for n in $TAPES; do
		wanted "$n" || continue
		run docker run --rm -v "$MOUNT:/vhs" "$VHS_IMAGE" "docs/assets/tapes/$n.tape" || exit 1
	done
}

step_diagramas() {
	echo "== Diagramas (d2) =="
	if [ -z "$DIAGRAMS" ]; then
		echo "No hay diagramas en docs/assets/diagrams/*.d2: nada que dibujar."
		return 0
	fi
	LOCAL_D2=0
	command -v d2 >/dev/null 2>&1 && LOCAL_D2=1
	if [ "$LOCAL_D2" = 0 ]; then
		check_docker
		need_image "$D2_IMAGE" "$D2_SIZE" "docker pull $D2_IMAGE"
	fi
	for n in $DIAGRAMS; do
		wanted "$n" || continue
		if [ "$LOCAL_D2" = 1 ]; then
			run d2 --theme 0 --dark-theme 200 --pad 24 "docs/assets/diagrams/$n.d2" "docs/assets/diagrams/$n.svg" || exit 1
		else
			run docker run --rm -v "$MOUNT:/work" -w /work "$D2_IMAGE" \
				--theme 0 --dark-theme 200 --pad 24 "docs/assets/diagrams/$n.d2" "docs/assets/diagrams/$n.svg" || exit 1
		fi
	done
}

step_videos() {
	echo "== Videos (HyperFrames) =="
	if [ -z "$VIDEOS" ]; then
		echo "No hay composiciones en docs/assets/video/*/index.html: nada que renderizar."
		return 0
	fi
	check_docker
	need_image "$VIDEO_IMAGE" "$VIDEO_SIZE" "$VIDEO_BUILD"
	mkdir -p docs/assets/video/out
	for n in $VIDEOS; do
		wanted "$n" || continue
		run docker run --rm --shm-size=1g -v "$MOUNT:/work" -w /work/docs/assets/video "$VIDEO_IMAGE" \
			hyperframes render "$n" -o "out/$n.mp4" --quiet || exit 1
	done
}

case "$GROUP" in
todo)
	step_build
	step_gifs
	step_diagramas
	step_videos
	;;
gifs)
	step_build
	step_gifs
	;;
diagramas) step_diagramas ;;
videos) step_videos ;;
esac
