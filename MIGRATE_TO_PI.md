# Migrate Spoolman to Raspberry Pi 4

## Current Pi State

- OS: Raspbian 10 (Buster), **32-bit** (`armv7l`) — needs reflash
- Disk: 28GB SD card, 20GB free
- Hostname: `raspberrypi4.local` (`192.168.1.169`)
- Existing stuff: old timelapse setup, Go, Python 3.10, Ansible, CTF scripts (all from 2021)

## Step 0: Reflash Pi with 64-bit OS

The Pi is currently running 32-bit Raspbian. The Spoolman Docker image needs `arm64`.

### Back up anything worth keeping

```bash
# From the Mac — grab anything you want to save before wiping
scp -r pi@raspberrypi4.local:~/ultimaker_timelapse/ ~/Desktop/pi-backup/
scp pi@raspberrypi4.local:~/pi-ultimaker ~/Desktop/pi-backup/
# Add any other files you want to keep
```

### Flash the SD card

1. Install Raspberry Pi Imager on Mac: `brew install --cask raspberry-pi-imager`
2. Remove the SD card from the Pi and put it in your Mac
3. Open Raspberry Pi Imager
4. Choose **Raspberry Pi OS (64-bit)** as the OS
5. Select the SD card
6. Click the gear icon to pre-configure:
   - Hostname: `raspberrypi4`
   - Enable SSH
   - Set username (`pi`) and password
   - WiFi credentials (if not using ethernet)
7. Flash and put the card back in the Pi

### Verify

```bash
ssh pi@raspberrypi4.local
uname -m  # Should show "aarch64"
```

## Step 1: Install Docker on the Pi

```bash
ssh pi@raspberrypi4.local
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER
# Log out and back in for group change to take effect
```

## Step 2: Stop Spoolman on Mac

```bash
cd ~/Projects/spoolman-docker
docker compose stop
```

Use `stop`, not `down` — `down` removes the container.

## Step 3: Copy files to the Pi

From the Mac:

```bash
scp -r ~/Projects/spoolman-docker/ pi@raspberrypi4.local:~/spoolman-docker/
```

This copies the compose file, database (`data/spoolman.db`), and backups.

## Step 4: Start Spoolman on the Pi

```bash
ssh pi@raspberrypi4.local
cd ~/spoolman-docker
docker compose up -d
```

The `ghcr.io/donkie/spoolman:latest` image supports `linux/arm64` natively.

Verify it's running: open `http://raspberrypi4.local:7912` in a browser.

## Step 5: Update fil config

Update the Spoolman URL in your fil config to point to the Pi instead of localhost:

```
spoolman_url: http://raspberrypi4.local:7912
```

Check which config file(s) fil is using and update accordingly.

## Step 6: Verify

```bash
fil find
```

Confirm fil can reach Spoolman on the Pi and your spool data is intact.

## Step 7: Clean up Mac

Once everything is confirmed working on the Pi:

```bash
cd ~/Projects/spoolman-docker
docker compose down
```

## Notes

- Database is SQLite, single file (`data/spoolman.db`, ~143KB) — easy to move
- Spoolman keeps its own backups in `data/backups/`
- The docker-compose.yaml is ready to use as-is on the Pi (no changes needed)
- Port mapping is `7912:8000` (host:container)
- Timezone is set to `America/Denver`
- Prometheus metrics are enabled
