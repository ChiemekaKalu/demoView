package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// source is MurkyYT/cs2-map-icons at the commit the R2 maps/ objects came from.
const source = "https://raw.githubusercontent.com/MurkyYT/cs2-map-icons/ae7ed6ac9bdebf7782c71ee2454f20738595e57e/"

// fetch downloads each map's radar and radar_info into dir. The commit is
// pinned, so a file already in dir is kept.
func fetch(dir string, maps []string, stdout io.Writer) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	client := &http.Client{Timeout: time.Minute}
	for _, m := range maps {
		if m == "" || strings.Trim(m, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" {
			return fmt.Errorf("bad map name %q", m)
		}
		for _, p := range []string{"images/radars/" + m + "_radar_psd.png", "data/radar_info/" + m + ".txt"} {
			dst := filepath.Join(dir, path.Base(p))
			if _, err := os.Stat(dst); err != nil {
				if err := download(client, source+p, dst); err != nil {
					return err
				}
			}
			fmt.Fprintln(stdout, dst)
		}
	}
	return nil
}

func download(client *http.Client, url, dst string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
