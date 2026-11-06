// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package radio

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// tune tells a running rigctld the frequency in hertz and the mode.
// Passband 0 asks the radio for its usual width for that mode.
// PTT is left alone so the modem can key the transmitter.
func tune(addr string, hz int64, mode string) error {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	rd := bufio.NewReader(conn)
	if err := command(conn, rd, fmt.Sprintf("F %d", hz)); err != nil {
		return fmt.Errorf("the radio rejected the frequency: %w", err)
	}
	if err := command(conn, rd, "M "+mode+" 0"); err != nil {
		return fmt.Errorf("the radio rejected the mode: %w", err)
	}
	return nil
}

func command(w io.Writer, rd *bufio.Reader, line string) error {
	if _, err := io.WriteString(w, line+"\n"); err != nil {
		return err
	}
	return readRPRT(rd)
}

func readRPRT(rd *bufio.Reader) error {
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "RPRT") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "0" {
			return nil
		}
		return fmt.Errorf("%s", line)
	}
}
