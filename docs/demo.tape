# Record weeklog week against the fictional 2026-W41 fixture.
# No tokens and no personal activity. From the repository root:
#   vhs docs/demo.tape

Output docs/demo.gif

Require go

Set Shell bash
Set FontFamily "JetBrains Mono"
Set FontSize 16
Set Columns 88
Set Rows 56
Set Padding 16
Set Margin 12
Set MarginFill "#11111b"
Set BorderRadius 8
Set WindowBar Colorful
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms
Set Framerate 20
Set CursorBlink false

Env TZ "UTC"
Env WEEKLOG_DEMO_HOME "/tmp/weeklog-demo"

Hide
Type@1ms "source docs/demo-setup.sh && clear"
Enter
Wait+Line@180s /^>$/
Show

Type "weeklog week --week 2026-W41"
Sleep 300ms
Enter
Wait+Screen@30s /Top PRs/
Sleep 2.5s
