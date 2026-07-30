$ErrorActionPreference = "Stop"

Add-Type -AssemblyName System.Drawing

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent (Split-Path -Parent $ScriptDir)
$StaticDir = Join-Path $ProjectRoot "src\interaction\web\static"

$Text = "ATM"
$Sizes = @(16, 32, 48, 64, 128, 256)
$PrimarySize = 256
$BgColor = [System.Drawing.Color]::FromArgb(255, 10, 10, 11)
$FgColor = [System.Drawing.Color]::FromArgb(255, 200, 169, 106)

function New-IconBitmap {
    param([int]$Size)

    $bitmap = New-Object System.Drawing.Bitmap($Size, $Size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    $graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $graphics.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::AntiAliasGridFit
    $graphics.Clear([System.Drawing.Color]::Transparent)

    $radius = [Math]::Max(1, [Math]::Round($Size * 6 / 32))
    $diameter = $radius * 2
    $rect = New-Object System.Drawing.RectangleF(0, 0, ($Size - 1), ($Size - 1))
    $path = New-Object System.Drawing.Drawing2D.GraphicsPath
    $path.AddArc($rect.X, $rect.Y, $diameter, $diameter, 180, 90)
    $path.AddArc(($rect.Right - $diameter), $rect.Y, $diameter, $diameter, 270, 90)
    $path.AddArc(($rect.Right - $diameter), ($rect.Bottom - $diameter), $diameter, $diameter, 0, 90)
    $path.AddArc($rect.X, ($rect.Bottom - $diameter), $diameter, $diameter, 90, 90)
    $path.CloseFigure()

    $bgBrush = New-Object System.Drawing.SolidBrush($BgColor)
    $graphics.FillPath($bgBrush, $path)

    $fontPath = "C:\Windows\Fonts\arialbd.ttf"
    if (-not (Test-Path $fontPath)) {
        $fontPath = "C:\Windows\Fonts\consolab.ttf"
    }
    $fontFamily = [System.Drawing.FontFamily]::GenericSansSerif
    if (Test-Path $fontPath) {
        $privateFonts = New-Object System.Drawing.Text.PrivateFontCollection
        $privateFonts.AddFontFile($fontPath)
        $fontFamily = $privateFonts.Families[0]
    }

    $fontSize = [Math]::Max(5, [Math]::Round($Size * 0.34))
    $font = New-Object System.Drawing.Font($fontFamily, $fontSize, [System.Drawing.FontStyle]::Bold, [System.Drawing.GraphicsUnit]::Pixel)
    $textBrush = New-Object System.Drawing.SolidBrush($FgColor)
    $format = New-Object System.Drawing.StringFormat
    $format.Alignment = [System.Drawing.StringAlignment]::Center
    $format.LineAlignment = [System.Drawing.StringAlignment]::Center
    $format.FormatFlags = [System.Drawing.StringFormatFlags]::NoWrap

    $textRect = New-Object System.Drawing.RectangleF(0, 0, $Size, ($Size - ($Size * 0.025)))
    $graphics.DrawString($Text, $font, $textBrush, $textRect, $format)

    $format.Dispose()
    $textBrush.Dispose()
    $font.Dispose()
    if ($privateFonts) { $privateFonts.Dispose() }
    $bgBrush.Dispose()
    $path.Dispose()
    $graphics.Dispose()

    return $bitmap
}

function ConvertTo-PngBytes {
    param([System.Drawing.Bitmap]$Bitmap)

    $stream = New-Object System.IO.MemoryStream
    try {
        $Bitmap.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)
        return ,$stream.ToArray()
    } finally {
        $stream.Dispose()
    }
}

if (-not (Test-Path $ScriptDir)) {
    New-Item -ItemType Directory -Path $ScriptDir | Out-Null
}
if (-not (Test-Path $StaticDir)) {
    New-Item -ItemType Directory -Path $StaticDir | Out-Null
}

$entries = @()
foreach ($size in $Sizes) {
    $bitmap = New-IconBitmap -Size $size
    try {
        $pngBytes = ConvertTo-PngBytes -Bitmap $bitmap
        $entries += [PSCustomObject]@{
            Size = $size
            Bytes = $pngBytes
        }

        if ($size -eq $PrimarySize) {
            $iconPng = Join-Path $ScriptDir "icon.png"
            $staticPng = Join-Path $StaticDir "metaatoms-icon.png"
            [System.IO.File]::WriteAllBytes($iconPng, $pngBytes)
            [System.IO.File]::WriteAllBytes($staticPng, $pngBytes)
        }
    } finally {
        $bitmap.Dispose()
    }
}

$icoPath = Join-Path $ScriptDir "icon.ico"
$writer = New-Object System.IO.BinaryWriter([System.IO.File]::Open($icoPath, [System.IO.FileMode]::Create))
try {
    $writer.Write([UInt16]0)
    $writer.Write([UInt16]1)
    $writer.Write([UInt16]$entries.Count)

    $offset = 6 + ($entries.Count * 16)
    foreach ($entry in $entries) {
        $encodedSize = $entry.Size
        if ($encodedSize -eq 256) {
            $encodedSize = 0
        }
        $writer.Write([byte]$encodedSize)
        $writer.Write([byte]$encodedSize)
        $writer.Write([byte]0)
        $writer.Write([byte]0)
        $writer.Write([UInt16]1)
        $writer.Write([UInt16]32)
        $writer.Write([UInt32]$entry.Bytes.Length)
        $writer.Write([UInt32]$offset)
        $offset += $entry.Bytes.Length
    }

    foreach ($entry in $entries) {
        $writer.Write($entry.Bytes)
    }
} finally {
    $writer.Dispose()
}

$rcPath = Join-Path $ScriptDir "icon.rc"
[System.IO.File]::WriteAllText($rcPath, "// MetaAtoms application icon resource`n// Regenerate: powershell -ExecutionPolicy Bypass -File build\assets\generate_icon.ps1`n1 ICON `"icon.ico`"`n", [System.Text.Encoding]::UTF8)

Write-Host "[ok] wrote $icoPath"
Write-Host "[ok] wrote $(Join-Path $ScriptDir 'icon.png')"
Write-Host "[ok] wrote $(Join-Path $StaticDir 'metaatoms-icon.png')"
Write-Host "[ok] wrote $rcPath"
