// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Jigarvarma2k20
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	source = "https://raw.githubusercontent.com/ipverse/as-metadata/master/as.json"
	dir    = "data"
)

var client *http.Client

type metadata struct {
	Handle      string `json:"handle"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

type entry struct {
	ASN      int      `json:"asn"`
	Metadata metadata `json:"metadata"`
}

func init() {
	client = &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
}

func fetch(url string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt < 5; attempt++ {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "ASN-Database-Updater/1.0")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()

			if readErr == nil && resp.StatusCode >= 200 &&
				resp.StatusCode < 300 {
				return body, nil
			}

			if readErr != nil {
				lastErr = readErr
			} else {
				lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			}

			if resp.StatusCode != 429 &&
				resp.StatusCode != 500 &&
				resp.StatusCode != 502 &&
				resp.StatusCode != 503 &&
				resp.StatusCode != 504 {
				return nil, lastErr
			}
		}

		if attempt < 4 {
			time.Sleep(time.Duration(1<<attempt) * time.Second)
		}
	}

	return nil, lastErr
}

func getEntries() ([]entry, error) {
	body, err := fetch(source)
	if err != nil {
		return nil, fmt.Errorf("download ASN metadata: %w", err)
	}

	var entries []entry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("parse ASN metadata: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("ASN metadata is empty")
	}
	return entries, nil
}

func fileName(category string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(category)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

func writeLines(path string, lines []string) error {
	sort.Slice(lines, func(i, j int) bool {
		a, _, _ := strings.Cut(lines[i], " #")
		b, _, _ := strings.Cut(lines[j], " #")
		ai, _ := strconv.Atoi(a)
		bi, _ := strconv.Atoi(b)
		return ai < bi
	})

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	w := bufio.NewWriterSize(file, 256*1024)
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return w.Flush()
}

func main() {
	start := time.Now()

	entries, err := getEntries()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("Found %d ASNs\n", len(entries))

	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(err)
	}

	output := make(map[string][]string)

	for _, e := range entries {
		category := strings.TrimSpace(e.Metadata.Category)
		if category == "" {
			category = "unknown"
		}

		name := strings.TrimSpace(e.Metadata.Handle)
		if name == "" {
			name = e.Metadata.Description
		}

		line := strconv.Itoa(e.ASN)
		if name != "" {
			line += " # " + name
		}
		output[fileName(category)] = append(output[fileName(category)], line)
	}

	fmt.Printf("Elapsed: %s\n", time.Since(start).Round(time.Second))

	kinds := make([]string, 0, len(output))
	for kind := range output {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	for _, kind := range kinds {
		path := dir + "/" + kind + ".txt"
		if err := writeLines(path, output[kind]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%s: %d entries\n", path, len(output[kind]))
	}
}
