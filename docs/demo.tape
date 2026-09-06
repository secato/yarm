# yarm demo recording, for https://github.com/charmbracelet/vhs
#
#   yay -S vhs          # or: go install github.com/charmbracelet/vhs@latest
#   go build -o /tmp/yarm-demo ./cmd/yarm
#   vhs docs/demo.tape
#
# The recording runs against a scratch YARM_HOME with one manually added
# game folder, so it does not depend on what is installed on the recording
# machine and does not touch a real game. It does hit the network once for
# the catalog; run it with a warm cache (start yarm yourself first) so the
# recording is not mostly a spinner.
#
# Sizes match the smallest layout the UI is tested against plus room to
# read: 100x30. Keep it under ~30s.

Output docs/demo.gif

Set Shell bash
Set FontSize 15
Set Width 1200
Set Height 700
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 60ms

# A scratch home with a fake game, so the demo is reproducible.
Hide
Type "export YARM_HOME=$(mktemp -d)/yarm" Enter
Type "mkdir -p /tmp/yarm-demo-game/Game && : > /tmp/yarm-demo-game/Game/eldenring.exe" Enter
Type "clear" Enter
Show

Type "yarm" Enter
Sleep 3s

# Games list: the welcome banner, then a game's folders and status.
Sleep 2s
Type "j"
Sleep 1s
Enter
Sleep 2s

# Install wizard: build and version side by side, then the steps.
Type "i"
Sleep 3s
Right
Sleep 1s
Enter
Sleep 2s

# API step — the detected graphics API is marked.
Sleep 2s
Enter
Sleep 2s

# Shaders: the curated shortlist, then the whole catalog.
Sleep 2s
Type "j"
Sleep 500ms
Space
Sleep 1s
Type "a"
Sleep 2s
Type "a"
Sleep 1s
Enter
Sleep 2s

# Add-ons: selecting AutoHDR pulls in the shader it requires.
Type "jjj"
Sleep 1s
Space
Sleep 2s
Enter
Sleep 3s

# Review: what will be downloaded, and the options.
Sleep 3s
Escape
Sleep 1s

Type "q"
Sleep 1s
