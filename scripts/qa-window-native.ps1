param(
    [Parameter(Mandatory=$true)][int]$TestProcessId,
    [ValidateSet('state','restore','resize','drag')][string]$Action = 'state'
)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$taskProcess = Get-Process -Id $TestProcessId
$taskExpected = (Resolve-Path -LiteralPath '.qa/ux-flow/test-app/uni-switch.exe').Path
if ($taskProcess.Path -ne $taskExpected) { throw 'Window QA must target the isolated test executable' }
Add-Type -TypeDefinition @'
using System;
using System.Text;
using System.Threading;
using System.Runtime.InteropServices;
public static class UniSwitchWindowQA {
    public delegate bool EnumProc(IntPtr h, IntPtr p);
    [StructLayout(LayoutKind.Sequential)] public struct Rect { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] public struct Point { public int X, Y; }
    [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc cb, IntPtr data);
    [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr h, StringBuilder s, int n);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern int GetClassName(IntPtr h, StringBuilder s, int n);
    [DllImport("user32.dll", EntryPoint="GetWindowLongPtrW")] public static extern IntPtr GetWindowLongPtr(IntPtr h, int n);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
    [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr h);
    [DllImport("user32.dll")] public static extern bool IsZoomed(IntPtr h);
    [DllImport("user32.dll")] static extern bool GetWindowRect(IntPtr h, out Rect r);
    [DllImport("user32.dll")] static extern bool ClientToScreen(IntPtr h, ref Point p);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int mode);
    [DllImport("user32.dll")] public static extern bool MoveWindow(IntPtr h, int x, int y, int w, int height, bool paint);
    [DllImport("user32.dll")] static extern bool SetForegroundWindow(IntPtr h);
    [DllImport("user32.dll")] static extern IntPtr GetForegroundWindow();
    [DllImport("user32.dll")] static extern bool SetCursorPos(int x, int y);
    [DllImport("user32.dll")] static extern bool GetCursorPos(out Point p);
    [DllImport("user32.dll")] static extern void mouse_event(uint flags, uint x, uint y, uint data, UIntPtr extra);
    public static IntPtr Find(int pid) {
        IntPtr result = IntPtr.Zero;
        EnumWindows((h, _) => {
            uint found; GetWindowThreadProcessId(h, out found);
            if (found != pid) return true;
            var title = new StringBuilder(256); GetWindowText(h,title,256);
            var cls = new StringBuilder(256); GetClassName(h,cls,256);
            if (title.ToString() == "uni-switch" && cls.ToString() == "Tauri Window") { result=h; return false; }
            return true;
        }, IntPtr.Zero);
        if (result == IntPtr.Zero) throw new Exception("Isolated Tauri window not found");
        return result;
    }
    public static Rect Bounds(IntPtr h) { Rect r; if (!GetWindowRect(h,out r)) throw new Exception("Bounds failed"); return r; }
    public static Point ClientOrigin(IntPtr h) { var p=new Point(); if (!ClientToScreen(h,ref p)) throw new Exception("Client origin failed"); return p; }
    public static void Drag(IntPtr h) {
        if (!IsWindowVisible(h) || IsIconic(h) || IsZoomed(h)) throw new Exception("Drag needs a visible restored window");
        SetForegroundWindow(h); Thread.Sleep(200);
        if (GetForegroundWindow()!=h) throw new Exception("QA window could not receive mouse input");
        var r=Bounds(h); Point prior; GetCursorPos(out prior);
        try {
            SetCursorPos(r.Left+400,r.Top+16); Thread.Sleep(100);
            mouse_event(2,0,0,0,UIntPtr.Zero); Thread.Sleep(200);
            for (int i=1;i<=8;i++) { SetCursorPos(r.Left+400+i*6,r.Top+16+i*3); Thread.Sleep(40); }
        } finally { mouse_event(4,0,0,0,UIntPtr.Zero); SetCursorPos(prior.X,prior.Y); }
        Thread.Sleep(150);
    }
}
'@
$taskWindow = [UniSwitchWindowQA]::Find($TestProcessId)
switch ($Action) {
    'restore' { [UniSwitchWindowQA]::ShowWindow($taskWindow,9) | Out-Null }
    'resize' { [UniSwitchWindowQA]::MoveWindow($taskWindow,80,80,1040,720,$true) | Out-Null }
    'drag' { [UniSwitchWindowQA]::Drag($taskWindow) }
}
$taskBounds = [UniSwitchWindowQA]::Bounds($taskWindow)
$taskClient = [UniSwitchWindowQA]::ClientOrigin($taskWindow)
$taskStyle = [UniSwitchWindowQA]::GetWindowLongPtr($taskWindow,-16).ToInt64()
[ordered]@{
    visible = [UniSwitchWindowQA]::IsWindowVisible($taskWindow)
    minimized = [UniSwitchWindowQA]::IsIconic($taskWindow)
    maximized = [UniSwitchWindowQA]::IsZoomed($taskWindow)
    caption = ($taskStyle -band 0x00C00000) -eq 0x00C00000
    resizeFrame = ($taskStyle -band 0x00040000) -ne 0
    nonClientTop = $taskClient.Y-$taskBounds.Top
    x = $taskBounds.Left; y = $taskBounds.Top
    width = $taskBounds.Right-$taskBounds.Left; height = $taskBounds.Bottom-$taskBounds.Top
} | ConvertTo-Json -Compress
