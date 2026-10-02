package notify

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// icon is the PNG of micko's notification icon: the site's favicon (see
// site/index.html), a cream rounded square with a red bar, on its 32-unit
// grid scaled to 512 pixels, the largest size sips writes as icns.
func icon() []byte {
	const size, unit = 512, 32
	cream := color.RGBA{0xef, 0xe4, 0xcf, 0xff}
	red := color.RGBA{0xca, 0x1b, 0x1d, 0xff}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	r := 6.0 * size / unit
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Distance past the rounded corner: the pixel centre against
			// the nearest corner circle's centre.
			fx, fy := float64(x)+0.5, float64(y)+0.5
			cx, cy := min(max(fx, r), size-r), min(max(fy, r), size-r)
			if (fx-cx)*(fx-cx)+(fy-cy)*(fy-cy) > r*r {
				continue
			}
			ux, uy := fx*unit/size, fy*unit/size
			if ux >= 9 && ux < 23 && uy >= 6 && uy < 26 {
				img.Set(x, y, red)
			} else {
				img.Set(x, y, cream)
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// bundleID is the identity macOS files micko's notifications under, with
// its own entry in System Settings → Notifications.
const bundleID = "io.github.ficaa1.micko"

const lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

var bundle struct {
	sync.Mutex
	path string
	err  error
	done bool
}

// micko returns the notifier inside micko's own app bundle, building the
// bundle from the terminal-notifier at tool the first time. macOS takes a
// notification's name and icon from the app that posts it, and offers no
// way to set them per notification, so a copy of terminal-notifier.app
// renamed to micko is the only way for a banner to say micko. A failed
// build is not retried in the same session.
func micko(tool string) (string, error) {
	bundle.Lock()
	defer bundle.Unlock()
	if !bundle.done {
		bundle.path, bundle.err = buildBundle(tool)
		bundle.done = true
	}
	return bundle.path, bundle.err
}

// buildBundle reuses the bundle when its stamp says it was built from the
// same terminal-notifier and icon, and rebuilds it otherwise. The stamp
// sits beside the bundle, not in it, so it does not touch the signature.
func buildBundle(tool string) (string, error) {
	resolved, err := filepath.EvalSymlinks(tool)
	if err != nil {
		return "", err
	}
	src := filepath.Join(filepath.Dir(filepath.Dir(resolved)), "terminal-notifier.app")
	if _, err := os.Stat(filepath.Join(src, "Contents", "Info.plist")); err != nil {
		return "", fmt.Errorf("no terminal-notifier.app beside %s", resolved)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "micko")
	app := filepath.Join(dir, "micko.app")
	bin := filepath.Join(app, "Contents", "MacOS", "terminal-notifier")
	pic := icon()
	sum := sha256.Sum256(pic)
	stamp := src + "\n" + hex.EncodeToString(sum[:8]) + "\n"
	stampFile := app + ".stamp"
	if got, err := os.ReadFile(stampFile); err == nil && string(got) == stamp {
		if _, err := os.Stat(bin); err == nil {
			return bin, nil
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp := app + ".new"
	_ = os.RemoveAll(tmp)
	defer os.RemoveAll(tmp)
	plist := filepath.Join(tmp, "Contents", "Info.plist")
	pngPath := filepath.Join(tmp, "Contents", "Resources", "micko.png")
	icns := filepath.Join(tmp, "Contents", "Resources", "micko.icns")
	if err := run("ditto", src, tmp); err != nil {
		return "", err
	}
	if err := run("chmod", "-R", "u+w", tmp); err != nil {
		return "", err
	}
	for key, value := range map[string]string{"CFBundleIdentifier": bundleID, "CFBundleName": "micko", "CFBundleIconFile": "micko"} {
		if err := run("plutil", "-replace", key, "-string", value, plist); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(pngPath, pic, 0o644); err != nil {
		return "", err
	}
	if err := run("sips", "-s", "format", "icns", pngPath, "--out", icns); err != nil {
		return "", err
	}
	if err := os.Remove(pngPath); err != nil {
		return "", err
	}
	if err := run("codesign", "--force", "--sign", "-", tmp); err != nil {
		return "", err
	}
	if err := os.RemoveAll(app); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, app); err != nil {
		return "", err
	}
	// ditto keeps terminal-notifier's dates, and macOS keeps serving a cached
	// icon for a bundle whose date has not moved, so the rebuild is dated now.
	now := time.Now()
	if err := os.Chtimes(app, now, now); err != nil {
		return "", err
	}
	// Registering tells Notification Center the bundle exists; without it
	// macOS refuses the bundle's first request to notify.
	_ = exec.Command(lsregister, "-f", app).Run()
	if err := os.WriteFile(stampFile, []byte(stamp), 0o644); err != nil {
		return "", err
	}
	return bin, nil
}

func run(name string, args ...string) error {
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, out)
	}
	return nil
}
