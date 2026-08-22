' Silent process kill for the SVC installer. Run via wscript.exe (a
' GUI-subsystem host => no console window ever flashes). Uses WMI to
' terminate the current and legacy app processes so the installer can
' overwrite the locked executable.
On Error Resume Next
Set wmi = GetObject("winmgmts:\\.\root\cimv2")
If Err.Number = 0 Then
  For Each n In Array("svc", "SDK Version Control")
    For Each p In wmi.ExecQuery("Select * From Win32_Process Where Name='" & n & ".exe'")
      p.Terminate
    Next
  Next
End If
