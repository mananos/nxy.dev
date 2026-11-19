#!/bin/sh
# Regenera los visuales de la documentacion: capturas de la TUI y portada (volcados de `nxy tui --demo --dump`
# sobre el marco de marca de docs/assets/brand/, con Chromium), diagramas (HTML -> PNG con Chromium) y
# videos (HyperFrames).
#
#   sh docs/assets/render.sh [--dry-run] [todo|capturas|diagramas|videos] [nombres...]
#
# Sin nombres regenera todo el grupo; con nombres, solo esos (una captura que cambia no reescribe las
# otras en el historial de git). Los volcados corren en el host con `go run`; el resto en Docker: nunca
# baja ni construye una imagen por su cuenta. Si falta una, imprime el comando exacto y sale con 2.
# Hay un equivalente para Windows PowerShell en docs/assets/render.ps1.

VIDEO_IMAGE="nxy-docs-video:0.9.10"
VIDEO_SIZE="unos 2 GB en disco (node, chromium, ffmpeg, fuentes y HyperFrames)"
VIDEO_BUILD="docker build -t nxy-docs-video:0.9.10 docs/assets/video"

DRY=0
if [ "$1" = "--dry-run" ]; then
	DRY=1
	shift
fi
GROUP="${1:-todo}"
[ $# -gt 0 ] && shift
NAMES="$*"

case "$GROUP" in
todo | capturas | diagramas | videos) ;;
*)
	echo "Grupo desconocido: $GROUP. Opciones: todo, capturas, diagramas, videos." >&2
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

TSV=docs/assets/capturas/capturas.tsv

# Listas de lo que existe en disco (un nombre por linea).
list_names() { # dir ext
	for f in "$1"/*."$2"; do
		[ -e "$f" ] || continue
		basename "$f" ".$2"
	done
}
list_captures() { # nombres del manifiesto, sin la cabecera
	[ -f "$TSV" ] || return 0
	tail -n +2 "$TSV" | tr -d '\r' | while IFS="$(printf '\t')" read -r n _; do
		[ -n "$n" ] && echo "$n"
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
CAPTURES=$(list_captures)
DIAGRAMS=$(list_names docs/assets/diagrams html)
VIDEOS=$(list_videos)

# Valida los nombres pedidos contra lo que existe para el grupo ("banner" es parte de las capturas).
case "$GROUP" in
capturas) VALID=$(printf '%s\nbanner\n' "$CAPTURES") ;;
diagramas) VALID="$DIAGRAMS" ;;
videos) VALID="$VIDEOS" ;;
*) VALID=$(printf '%s\nbanner\n%s\n%s\n' "$CAPTURES" "$DIAGRAMS" "$VIDEOS") ;;
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

step_capturas() {
	echo "== Capturas de la TUI y portada =="
	WANTED=""
	for n in $CAPTURES; do
		wanted "$n" && WANTED="$WANTED $n"
	done
	WANT_BANNER=0
	wanted banner && WANT_BANNER=1
	if [ -z "$WANTED" ] && [ "$WANT_BANNER" = 0 ]; then
		echo "Nada que capturar."
		return 0
	fi
	check_docker
	need_image "$VIDEO_IMAGE" "$VIDEO_SIZE" "$VIDEO_BUILD"
	[ "$DRY" = 1 ] || mkdir -p docs/assets/.build/dumps docs/assets/capturas
	# 1) volcados de la TUI con datos de ejemplo, en el host (mismo resultado en Windows y Linux)
	for n in $WANTED; do
		dump=$(tail -n +2 "$TSV" | tr -d '\r' | awk -F '\t' -v n="$n" '$1 == n { print $2 }')
		feat=$(tail -n +2 "$TSV" | tr -d '\r' | awk -F '\t' -v n="$n" '$1 == n { print $3 }')
		FEAT_ARGS=""
		[ "$feat" != "-" ] && [ -n "$feat" ] && FEAT_ARGS="--feature $feat"
		echo "+ go run ./cmd/nxy tui --demo --dump $dump $FEAT_ARGS --width 120 --icons unicode > docs/assets/.build/dumps/$n.ansi"
		if [ "$DRY" != 1 ]; then
			# shellcheck disable=SC2086
			go run ./cmd/nxy tui --demo --dump "$dump" $FEAT_ARGS --width 120 --icons unicode >"docs/assets/.build/dumps/$n.ansi" || exit 1
			if ! grep -q "$(printf '\033')\[38;2" "docs/assets/.build/dumps/$n.ansi"; then
				echo "El volcado de $n no trae color (ESC[38;2): sin color la captura no sirve." >&2
				exit 1
			fi
		fi
	done
	# 2) capturas y portada (Chromium)
	if [ -n "$WANTED" ]; then
		# shellcheck disable=SC2086
		run docker run --rm --shm-size=1g -v "$MOUNT:/work" -w /work "$VIDEO_IMAGE" node docs/assets/brand/captura.cjs captura $WANTED || exit 1
	fi
	if [ "$WANT_BANNER" = 1 ]; then
		run docker run --rm --shm-size=1g -v "$MOUNT:/work" -w /work "$VIDEO_IMAGE" node docs/assets/brand/captura.cjs banner || exit 1
	fi
}

step_diagramas() {
	echo "== Diagramas (HTML -> PNG) =="
	if [ -z "$DIAGRAMS" ]; then
		echo "No hay diagramas en docs/assets/diagrams/*.html: nada que dibujar."
		return 0
	fi
	WANTED=""
	for n in $DIAGRAMS; do
		wanted "$n" && WANTED="$WANTED $n"
	done
	[ -z "$WANTED" ] && return 0
	check_docker
	need_image "$VIDEO_IMAGE" "$VIDEO_SIZE" "$VIDEO_BUILD"
	# shellcheck disable=SC2086
	run docker run --rm --shm-size=1g -v "$MOUNT:/work" -w /work "$VIDEO_IMAGE" node docs/assets/brand/captura.cjs diagrama $WANTED || exit 1
}

step_videos() {
	echo "== Videos (HyperFrames) =="
	if [ -z "$VIDEOS" ]; then
		echo "No hay composiciones en docs/assets/video/*/index.html: nada que renderizar."
		return 0
	fi
	check_docker
	need_image "$VIDEO_IMAGE" "$VIDEO_SIZE" "$VIDEO_BUILD"
	for n in $VIDEOS; do
		wanted "$n" || continue
		# HyperFrames puede no servir archivos fuera de la carpeta de la composicion: la marca se copia antes
		# del render (marca/ esta en .gitignore) y se borra despues.
		if [ "$DRY" = 1 ]; then
			echo "+ cp docs/assets/brand/marca.css docs/assets/brand/perro-*.png docs/assets/video/$n/marca/"
		else
			mkdir -p "docs/assets/video/$n/marca"
			cp docs/assets/brand/marca.css docs/assets/video/"$n"/marca/
			for p in perro-banner perro-chico perro-diagrama; do cp "docs/assets/brand/$p.png" "docs/assets/video/$n/marca/"; done
		fi
		run docker run --rm --shm-size=1g -v "$MOUNT:/work" -w /work/docs/assets/video "$VIDEO_IMAGE" \
			hyperframes render "$n" -o "$n.mp4" --quiet
		rc=$?
		[ "$DRY" = 1 ] || rm -rf "docs/assets/video/$n/marca"
		[ "$rc" = 0 ] || exit 1
	done
}

case "$GROUP" in
todo)
	step_capturas
	step_diagramas
	step_videos
	;;
capturas) step_capturas ;;
diagramas) step_diagramas ;;
videos) step_videos ;;
esac
