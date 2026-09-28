# Windows / macOS Integration Guide

**These are procedures awaiting acceptance tests on actual machines, not a completed compatibility certification.** Local `dist/` builds or GitHub Releases provide unsigned Windows/macOS binaries; they still need to be started and tested on each target system. Electron documentation says `--proxy-server` applies to HTTP/HTTPS/WebSocket, but this does not guarantee every Discord distribution accepts or preserves the Chromium arguments it receives. [S3] Chrome's manual HTTP proxy supports CONNECT and WebSocket routing. [S2] See `SOURCES.md` for reference links.

Start with a test account and test channel containing no sensitive content. Do not use a long-running session from an existing work or personal account for the first test.

## 1. Dedicated directory and root CA

Use a dedicated directory on an encrypted local disk, outside shared or synced drives and locations writable by other accounts. Examples are `diskord-runtime` under a Windows user directory or `$HOME/diskord-runtime` on macOS. Running diskord itself does not require administrator privileges.

After copying the target binary into that directory, run `init`, `ca issue`, and `ca select`. The runtime directory must be writable. OpenSSL must be on PATH or specified by its actual executable path. Windows is not assumed to include OpenSSL; issuance fails by design when it is missing. Use a toolchain you already installed and trust. Once the certificate has been issued, normal operation does not call OpenSSL again.

### Windows: manually trust the certificate

First inspect the SHA-256 fingerprint shown by `diskord ca issue/select` and verify the file in the system certificate viewer. You can run `certmgr.msc` and import **the public certificate at ca/root.pem** under “Current User → Trusted Root Certification Authorities → Certificates.” UI labels and locations may differ by version or organizational policy. Verify the certificate and fingerprint, and never import the private key.

After verifying it, you can explicitly import it into the current user's store with:

```powershell
certutil -user -addstore Root .\ca\root.pem
```

This is a manual grant of trust by the user; diskord does not perform it automatically. Organizationally managed machines may prohibit this operation. Do not disable certificate verification or bypass administrative policy to proceed. To stop using it, locate the CA by fingerprint in the certificate manager and remove it.

### macOS: manually trust the certificate

Open the public root certificate with Keychain Access, verify its SHA-256 fingerprint, and import it into the current user's login keychain. In that certificate's trust settings, set the SSL trust level you explicitly approve. System authentication may be required, and organizational policy may restrict the change. Change only this newly created CA, not other system roots.

Fully restart the test client so it can see the new trust state. To stop using it, delete the corresponding root certificate by fingerprint. The absence of certificate errors alone is not proof that capture works; also check the entities and Gateway counters.

## 2. Separate Chrome profile

Do not reuse a running Chrome profile; an existing instance might take over the launch request and ignore the arguments. The examples below place the test profile inside the runtime directory to keep client data together. Chrome still manages the profile, so this does not imply that diskord controls every OS file Chrome writes.

macOS example:

```sh
R="$HOME/diskord-runtime"
mkdir -p "$R/profiles/chrome"
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
  --user-data-dir="$R/profiles/chrome" \
  --proxy-server="http://127.0.0.1:3901" \
  --disable-quic \
  "https://discord.com/app"
```

Windows PowerShell example; replace the path with the actual Chrome installation path:

```powershell
$R = Join-Path $HOME 'diskord-runtime'
New-Item -ItemType Directory -Force (Join-Path $R 'profiles\chrome') | Out-Null
$Chrome = 'C:\Program Files\Google\Chrome\Application\chrome.exe'
& $Chrome "--user-data-dir=$R\profiles\chrome" '--proxy-server=http://127.0.0.1:3901' '--disable-quic' 'https://discord.com/app'
```

`--disable-quic` keeps initial testing focused on the TCP proxy; it does not disable TLS verification. The effect of arguments may still vary across Chromium builds. Do not add `--ignore-certificate-errors`, `--no-sandbox`, or network/TLS dump arguments. This project does not provide those bypasses.

Visit the test channel, send a message with a unique marker, and search for it in the console. An increasing HTTP counter with a zero Gateway counter does not establish that real-time WS capture works. Check the proxy arguments, whether the client was fully restarted, whether Gateway was parsed as JSON or supported ETF, and the “unsupported WS encoding/lost synchronization” counters.

## 3. Discord desktop client

First exit completely, including the Windows tray app or any process still running on macOS. Closing only the window usually does not guarantee launch arguments are reapplied. After confirming exit, start the actual Discord program, not an updater or old shortcut that may swallow arguments.

On macOS, search the usual system and user application folders, then let Launch Services start the app independently of this terminal:

```sh
found=0
for app in /Applications/Discord.app "$HOME/Applications/Discord.app"; do
  if [ -x "$app/Contents/MacOS/Discord" ]; then
    open -a "$app" --args --proxy-server=http://127.0.0.1:3901 --disable-quic
    found=1
    break
  fi
done
[ "$found" -eq 1 ] || echo 'Discord.app was not found in the usual application folders.' >&2
```

On Windows, search the per-user versioned installation and common machine-wide locations, then launch the real `Discord.exe` rather than `Update.exe`:

```powershell
$candidates = @(
  Get-ChildItem "$env:LOCALAPPDATA\Discord\app-*\Discord.exe" -ErrorAction SilentlyContinue
  Get-Item "$env:ProgramFiles\Discord\Discord.exe" -ErrorAction SilentlyContinue
  Get-Item "${env:ProgramFiles(x86)}\Discord\Discord.exe" -ErrorAction SilentlyContinue
) | Where-Object { $_ }
$Discord = $candidates | Sort-Object LastWriteTime -Descending | Select-Object -First 1
if (-not $Discord) { throw 'Discord.exe was not found in the usual installation folders.' }
Start-Process -FilePath $Discord.FullName -ArgumentList '--proxy-server=http://127.0.0.1:3901','--disable-quic'
```

These commands are integration attempts, not tested guarantees. Client updates, release channels, organizational policies, or built-in network implementations may change behavior. If the client rejects the CA, ignores the proxy, or uses an unsupported encoding, record its version and nonsensitive counters and stop acceptance testing for that path. JSON and the supported subset of ETF Gateway events are observed without changing the client's negotiated encoding. Do not inject into the process, modify the client, bypass pinning, or disable TLS verification.

The application does not automatically change Discord's own user-data path, collect UDP voice/video/WebRTC traffic, or guarantee that every desktop network request uses Chromium's proxy layer. The console shows only data that actually passed through a supported capture path.

## 4. Troubleshooting order

Run `doctor` first to check the YAML, paths, toolchain, and CA validity period. `doctor` does not prove that trust or client routing works. Then check that ports 3900 and 3901 are free, confirm you can log in to the console, verify Chrome with the explicit profile and command line, and finally test the Discord desktop client.

Also inspect the console counters for parsed HTTP, parsed Gateway events, current WS connections, queue drops, decode errors, and storage errors. Do not export real tokens or raw payloads for troubleshooting. Unauthenticated requests, APIs outside the allowlist, content served from a browser cache/Service Worker, and old connections that have not reconnected may produce no new capture.

After changing the CA, update manual trust and reconnect. Changing the resource toggle affects subsequent processing and new CDN CONNECT sessions. Existing TLS sessions cannot have their decryption “undone”; once resource capture is disabled, subsequent responses on those sessions also stop entering the cache.
