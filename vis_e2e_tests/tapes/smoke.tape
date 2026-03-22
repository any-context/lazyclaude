Output outputs/smoke/smoke.gif
Output outputs/smoke/smoke.txt

Set Shell "bash"
Set Width 1800
Set Height 1200
Set FontSize 16
Set WaitTimeout 5s

Type "pwd" Enter Wait+Screen /app/
Type "ls" Enter Wait+Screen /go\.mod/
Type "echo hello world" Enter Wait+Screen /hello world/
Type "cat /nonexistent" Enter Wait+Screen /No such file/
Type "ls /no/such/dir" Enter Wait+Screen /cannot access/
