#!/bin/sh
# Copy the Phosphor icons (https://phosphoricons.com, MIT) that inua-shell
# draws into ui/icons, named as the freedesktop icons the shell asks for.
# The shell puts them in an icon theme of its own (style.icons = "phosphor").
# Edit the list and run again to change or add one; the files are committed.
#
#   scripts/phosphor-icons.sh path/to/node_modules/@phosphor-icons/core
#
# Regular weight (npm i @phosphor-icons/core).
set -eu
src=${1:?usage: $0 PHOSPHOR_CORE_DIR (node_modules/@phosphor-icons/core)}
dest=$(CDPATH= cd -- "$(dirname -- "$0")/../ui/icons" && pwd)
rm -f "$dest"/*.svg
cp "$src/LICENSE" "$dest/LICENSE"

# freedesktop name (without -symbolic)   Phosphor name
while read -r name icon; do
	case "$name" in '' | '#'*) continue ;; esac
	cp "$src/assets/regular/$icon.svg" "$dest/$name-symbolic.svg"
done <<'LIST'
# bar and control center: network
network-offline                       globe-simple-x
network-vpn                           shield-check
network-wired                         network
network-wireless-disabled             wifi-slash
network-wireless-offline              wifi-x
network-wireless-signal-excellent     wifi-high
network-wireless-signal-good          wifi-medium
network-wireless-signal-ok            wifi-medium
network-wireless-signal-weak          wifi-low
network-wireless-signal-none          wifi-none
bluetooth-active                      bluetooth
bluetooth-disabled                    bluetooth-slash
# sound and brightness
audio-volume-muted                    speaker-x
audio-volume-low                      speaker-low
audio-volume-medium                   speaker-low
audio-volume-high                     speaker-high
display-brightness                    sun
# battery, in steps of ten (shell.BatteryIcon); Phosphor has one charging icon
battery-level-0                       battery-empty
battery-level-10                      battery-low
battery-level-20                      battery-low
battery-level-30                      battery-medium
battery-level-40                      battery-medium
battery-level-50                      battery-medium
battery-level-60                      battery-high
battery-level-70                      battery-high
battery-level-80                      battery-high
battery-level-90                      battery-full
battery-level-100                     battery-full
battery-level-100-charged             battery-full
battery-level-0-charging              battery-charging
battery-level-10-charging             battery-charging
battery-level-20-charging             battery-charging
battery-level-30-charging             battery-charging
battery-level-40-charging             battery-charging
battery-level-50-charging             battery-charging
battery-level-60-charging             battery-charging
battery-level-70-charging             battery-charging
battery-level-80-charging             battery-charging
battery-level-90-charging             battery-charging
battery-level-100-charging            battery-charging
# media
audio-x-generic                       music-note
media-playback-start                  play
media-playback-pause                  pause
media-skip-backward                   skip-back
media-skip-forward                    skip-forward
# control center buttons and tiles
system-lock-screen                    lock
dialog-password                       key
edit-paste                            clipboard-text
applets-screenshooter                 selection
emblem-system                         gear-six
power-profile-power-saver             leaf
weather-clear-night                   moon
# notifications
preferences-system-notifications      bell
window-close                          x
edit-undo                             arrow-counter-clockwise
# launcher and settings
application-x-executable              app-window
edit-clear                            x
pan-down                              caret-down
pan-up                                caret-up
# screenshot tool
edit-select-all                       selection
focus-windows                         app-window
video-display                         monitor
# light apps
alarm                                 alarm
weather-clear                         sun
weather-few-clouds                    cloud-sun
weather-few-clouds-night              cloud-moon
weather-overcast                      cloud
weather-fog                           cloud-fog
weather-showers-scattered             cloud-rain
weather-showers                       cloud-rain
weather-snow                          cloud-snow
weather-storm                         cloud-lightning
weather-severe-alert                  warning
go-previous                           caret-left
go-next                               caret-right
list-add                              plus
accessories-calculator                calculator
view-list                             list
# search entries
system-search                         magnifying-glass
edit-find                             magnifying-glass
# the dock's drawer and the control center's power row
view-app-grid                         squares-four
system-suspend                        moon
system-reboot                         arrow-clockwise
system-shutdown                       power
system-log-out                        sign-out
# color picker and recording
edit-copy                             copy
color-select                          eyedropper
media-record                          record
# image viewer
zoom-in                               magnifying-glass-plus
zoom-out                              magnifying-glass-minus
object-rotate-right                   arrow-clockwise
user-trash                            trash
folder-open                           folder-open
folder                                folder
view-fullscreen                       corners-out
view-restore                          corners-in
dialog-information                    info
preferences-desktop-wallpaper         image-square
image-x-generic                       image
# calculator
calc-backspace                        backspace
calc-plus-minus                       plus-minus
document-open-recent                  clock-counter-clockwise
LIST
echo "$(ls "$dest"/*.svg | wc -l) icons in $dest"
