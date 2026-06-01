#!/usr/bin/env bash
# record-example.sh — record a leather example as an asciinema cast and
# render an animated SVG with the Leather Graphite palette + window chrome.
#
# Usage:
#   scripts/record-example.sh NN [extra-make-args...]
#
# Examples:
#   scripts/record-example.sh 01
#   scripts/record-example.sh 02
#   scripts/record-example.sh 09-live
#
# Output:
#   recordings/<NN>-<timestamp>.cast   # raw asciinema recording (JSON, editable)
#   recordings/<NN>-<timestamp>.svg    # animated SVG, brand-colored, README-ready
#
# Need a gif or mp4? Both can be re-derived from the .cast on demand:
#   agg --theme dracula recordings/foo.cast recordings/foo.gif
#   ffmpeg -i recordings/foo.gif -vf 'fps=30,scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p' \
#     -c:v libx264 -movflags +faststart recordings/foo.mp4
#
# Workflow:
#   1. Script clears the screen and starts `asciinema rec`.
#   2. You see a clean prompt — type `make example-NN` (already in scrollback).
#      Just press Enter, or hit Ctrl-D at any time to stop early.
#   3. When the example finishes (or you Ctrl-D), the script scrubs the cast
#      (rewriting the absolute repo path to `leather`) and renders the SVG.
#
# Requires: asciinema, svg-term-cli.
#   Arch:   sudo pacman -S asciinema && npm install -g svg-term-cli
#   Other:  see https://asciinema.org and https://github.com/marionebl/svg-term-cli

set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 NN [extra-make-args...]" >&2
  exit 2
fi

NN="$1"; shift
MAKE_ARGS=("$@")

# Sanity-check tools.
missing=()
for t in asciinema svg-term; do
  command -v "$t" >/dev/null 2>&1 || missing+=("$t")
done
if (( ${#missing[@]} > 0 )); then
  echo "missing tools: ${missing[*]}" >&2
  echo "install them and retry. see header of $0 for hints." >&2
  exit 1
fi

# Resolve repo root from this script's location so it works from anywhere.
# Use ${BASH_SOURCE[0]:-$0} so the same expression works when the script is
# accidentally invoked as `zsh scripts/record-example.sh ...` instead of
# letting the bash shebang run it. Under zsh, BASH_SOURCE is unset and $0
# holds the script path.
SCRIPT_PATH="${BASH_SOURCE[0]:-$0}"
SCRIPT_DIR="$(cd -- "$(dirname -- "${SCRIPT_PATH}")" &> /dev/null && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUT_DIR="${REPO_ROOT}/recordings"
mkdir -p "${OUT_DIR}"

TS="$(date +%Y%m%d-%H%M%S)"
BASENAME="${NN}-${TS}"
CAST="${OUT_DIR}/${BASENAME}.cast"
SVG="${OUT_DIR}/${BASENAME}.svg"

# Render knobs — tweak via env.
#
# The SVG render uses the Leather Graphite konsole color scheme shipped at
# scripts/recording/leather-graphite.colorscheme so the bg/fg/accents match
# the brand exactly. (svg-term-cli requires the --profile path to start with
# `/`, `~`, or `.`; anything else is silently treated as a preset name and
# falls back to defaults.)
FONT_SIZE="${LEATHER_RECORD_FONT_SIZE:-22}"
# Recording geometry — smaller cell grid + larger font keeps text legible in
# the rendered SVG (the old 152x42 / 18pt was unreadable when scaled into
# a README). Bump COLS/ROWS if a particular example needs more space.
COLS="${LEATHER_RECORD_COLS:-160}"
ROWS="${LEATHER_RECORD_ROWS:-30}"

# The command we want the user to run inside the recording. Pre-populating the
# scrollback (echoed before asciinema starts) is friendlier than typing it
# blind inside the recorded shell.
CMD=(make "example-${NN}" "${MAKE_ARGS[@]}")

cat <<EOF

Recording example ${NN}.
Output base:  ${OUT_DIR}/${BASENAME}.{cast,svg}
Geometry=${COLS}x${ROWS}  font=${FONT_SIZE}pt

When the recorded shell opens:
  1. Run:   ${CMD[*]}
  2. Press Ctrl-D when finished (or wait for the example to exit).

Starting in 2s...
EOF
sleep 2

# Run asciinema with a clean cwd + minimal prompt. --overwrite is safe because
# CAST is timestamped. We use the user's $SHELL but with a tiny PROMPT for
# readability in the GIF.
ASCIINEMA_PROMPT='%F{#af5fff}leather $%f '
cd "${REPO_ROOT}"

# Write a one-shot zshrc that sources the user's real zsh dotfiles, then
# overrides the prompt and prints a hint line. Cleaned up on exit.
#
# Why source the real dotfiles: setting ZDOTDIR to a tempdir means zsh skips
# the user's normal $HOME/.zshrc entirely (it reads $ZDOTDIR/.zshrc instead).
# Without this re-source, the recording shell has no aliases, no prompt
# theme, no asdf shims, etc. — a "brand new uncustomized zsh".
TMPRC_DIR="$(mktemp -d -t leather-record-zdotdir.XXXXXX)"
trap 'rm -rf "${TMPRC_DIR}"' EXIT
REAL_ZDOTDIR="${ZDOTDIR:-$HOME}"
cat > "${TMPRC_DIR}/.zshrc" <<RC
# Pull in the operator's normal zsh environment first.
[[ -f "${REAL_ZDOTDIR}/.zshrc" ]] && source "${REAL_ZDOTDIR}/.zshrc"
# Force cwd back to the repo root in case the operator's .zshrc cd's
# somewhere else (chpwd hooks, default-dir tools, asdf, etc.).
cd "${REPO_ROOT}"
# Then override for clean recording output.
PROMPT='${ASCIINEMA_PROMPT}'
RPROMPT=''
unset RPS1 RPS2 2>/dev/null
# Clear scrollback so the recorded shell starts on a blank canvas. We do NOT
# print a hint line here — the pre-record banner above already told the
# operator what to type, and any in-shell print would get baked into the SVG
# (and in practice gets lumped into the same write event as the prompt, so
# trying to scrub it later removes the prompt too).
clear
RC

ZDOTDIR="${TMPRC_DIR}" \
  asciinema rec \
    --overwrite \
    --output-format asciicast-v2 \
    --window-size "${COLS}x${ROWS}" \
    --title "leather example ${NN}" \
    --command "zsh -i" \
    "${CAST}"

# Note: we don't auto-run the command — the operator types it (or Up-arrow,
# since it's in the hint line). This keeps the recording natural-paced and
# lets you abort cleanly if something looks wrong.

# Scrub the cast: rewrite the absolute repo path to a friendly relative form
# so renders never leak the operator's $HOME layout.
echo
echo "Scrubbing paths in: ${CAST}"
sed -i "s|${REPO_ROOT}|leather|g" "${CAST}"

# Render the animated SVG with the Leather Graphite konsole profile and
# traffic-light window chrome. svg-term-cli requires an absolute or `.`-prefixed
# profile path; the repo-rooted path below satisfies that.
PROFILE="${SCRIPT_DIR}/recording/leather-graphite.colorscheme"
if [[ ! -f "${PROFILE}" ]]; then
  echo "missing color profile: ${PROFILE}" >&2
  exit 1
fi

echo "Rendering SVG: ${SVG}"
svg-term \
  --in "${CAST}" \
  --out "${SVG}" \
  --term konsole --profile "${PROFILE}" \
  --window \
  --padding 12 \
  --width "${COLS}" --height "${ROWS}"

# Post-process the rendered SVG:
#   1. Strip svg-term-cli's hard-coded macOS traffic-light chrome
#      (#ff5f58 / #ffbd2e / #18c132) so the frame stays minimal/konsole-ish.
#   2. Add text-rendering="optimizeLegibility" on the root svg so Chrome keeps
#      font hinting (the previous attempt used geometricPrecision, which turns
#      hinting OFF and made the text look blurry at 100% zoom).
#   3. Put shape-rendering="crispEdges" only on the outer frame rect — putting
#      it on the root svg would cascade onto every glyph and undo (2).
sed -i \
  -e 's|<svg y="0%" x="0%"><circle[^<]*/><circle[^<]*/><circle[^<]*/></svg>||' \
  -e 's|<svg xmlns="http://www.w3.org/2000/svg"|<svg text-rendering="optimizeLegibility" xmlns="http://www.w3.org/2000/svg"|' \
  -e 's| class="a"/>| shape-rendering="crispEdges" class="a"/>|' \
  "${SVG}"

# Also render a pixel-aligned GIF via agg. svg-term-cli's animated SVG output
# uses a 1.002 cell-stride kerning factor that produces sub-pixel glyph
# positions (e.g. x="8.016") which Chromium anti-aliases — looks blurry at
# 100% zoom no matter what text-rendering hints we add. agg rasterises to a
# fixed pixel grid, so each glyph lands on an integer pixel and stays sharp.
# README embeds can pick whichever artifact they prefer.
GIF="${SVG%.svg}.gif"
if command -v agg >/dev/null 2>&1; then
  echo "Rendering GIF: ${GIF}"
  AGG_FONT_SIZE="${AGG_FONT_SIZE:-${FONT_SIZE}}"
  # Leather Graphite palette as agg `--theme custom` triplets:
  #   bg fg c0..c7 c8..c15  (18 hex values, no `#`)
  # Mirrors scripts/recording/leather-graphite.colorscheme exactly so the
  # rasterised GIF matches the SVG colour-for-colour.
  AGG_THEME="232323,e8e6e3,\
26242c,ff6b7a,10a889,d9b86c,35b8f0,8b5cf6,38d0c8,e8e6e3,\
6e6a76,ff8490,22c7a5,f0d58a,54c7ff,a78bfa,6be8df,ffffff"
  # Mirror svg-term-cli's font stack so glyph metrics match the SVG output.
  # `--text-font-family` (not `--font-family`) lets agg keep its bundled
  # Nerd-Font / emoji fallbacks for any glyphs not in the primary list.
  AGG_TEXT_FONTS="Monaco,Consolas,Menlo,Bitstream Vera Sans Mono,DejaVu Sans Mono,Liberation Mono"
  agg \
    --text-font-family "${AGG_TEXT_FONTS}" \
    --font-size "${AGG_FONT_SIZE}" \
    --theme "${AGG_THEME}" \
    --speed 1 \
    "${CAST}" "${GIF}" || echo "agg render failed (non-fatal); SVG still produced"
else
  echo "agg not installed — skipping GIF render (install: cargo install --git https://github.com/asciinema/agg)"
fi

echo
echo "Done."
ls -lh "${CAST}" "${SVG}" "${GIF}" 2>/dev/null || ls -lh "${CAST}" "${SVG}"
