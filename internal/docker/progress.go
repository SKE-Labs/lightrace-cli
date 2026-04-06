package docker

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type pullEvent struct {
	Status         string          `json:"status"`
	ID             string          `json:"id"`
	ProgressDetail *progressDetail `json:"progressDetail"`
}

type progressDetail struct {
	Current int64 `json:"current"`
	Total   int64 `json:"total"`
}

type layerProgress struct {
	current int64
	total   int64
	done    bool
}

func renderPullProgress(r io.Reader, imageName string) error {
	decoder := json.NewDecoder(r)
	layers := make(map[string]*layerProgress)
	tty := isTerminal()
	name := shortName(imageName)

	for {
		var evt pullEvent
		if err := decoder.Decode(&evt); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		switch {
		case strings.HasPrefix(evt.Status, "Status: Image is up to date"):
			clearLine(tty)
			fmt.Printf("  %s — up to date\n", name)
			return nil

		case strings.HasPrefix(evt.Status, "Status: Downloaded newer image"):
			clearLine(tty)
			fmt.Printf("  %s — done\n", name)
			return nil

		case evt.Status == "Pulling fs layer":
			layers[evt.ID] = &layerProgress{}

		case evt.Status == "Downloading":
			if lp, ok := layers[evt.ID]; ok && evt.ProgressDetail != nil {
				lp.current = evt.ProgressDetail.Current
				lp.total = evt.ProgressDetail.Total
			}

		case evt.Status == "Download complete" || evt.Status == "Pull complete" || evt.Status == "Already exists":
			if lp, ok := layers[evt.ID]; ok {
				lp.done = true
				if lp.total > 0 {
					lp.current = lp.total
				}
			}
		}

		if tty && len(layers) > 0 {
			var totalBytes, currentBytes int64
			var doneCount int
			for _, lp := range layers {
				totalBytes += lp.total
				currentBytes += lp.current
				if lp.done {
					doneCount++
				}
			}
			renderBar(name, currentBytes, totalBytes, doneCount, len(layers))
		}
	}

	clearLine(tty)
	fmt.Printf("  %s — done\n", name)
	return nil
}

func renderBar(name string, current, total int64, done, count int) {
	const barWidth = 30

	var pct float64
	if total > 0 {
		pct = float64(current) / float64(total)
		if pct > 1 {
			pct = 1
		}
	} else if done == count && count > 0 {
		pct = 1
	}

	filled := int(pct * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	bar := strings.Repeat("\u2588", filled) + strings.Repeat("\u2591", barWidth-filled)

	sizeStr := ""
	if total > 0 {
		sizeStr = fmt.Sprintf(" %s/%s", humanizeBytes(current), humanizeBytes(total))
	}

	fmt.Printf("\r  %-25s [%s] %d/%d layers%s", name, bar, done, count, sizeStr)
}

func clearLine(tty bool) {
	if tty {
		fmt.Print("\r\033[K")
	}
}

func humanizeBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

func shortName(image string) string {
	parts := strings.Split(image, "/")
	return parts[len(parts)-1]
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
