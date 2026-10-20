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
	"bytes"
	"compress/gzip"
	"encoding/csv"
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
	dir = "data"

	// Source: InternetData ASN database.
	asnDatabaseURL = "https://internetdata.io/api/v2/database/download?id=asn_v1&format=csvgz"
	apiKeyEnv      = "INTERNETDATA_APIKEY"

	// Whitelist of ASNs to exclude from hosting.txt, one ASN per line.
	hostingWhitelist = "whitelist_hosting.asn"
)

// categoryFiles maps the InternetData `type` to the file each ASN is written to.
var categoryFiles = map[string]string{
	"hosting":    "hosting",
	"isp":        "isp",
	"business":   "business",
	"education":  "education_research",
	"government": "government_admin",
}

var client *http.Client

type asnRecord struct {
	asn  int
	name string
	kind string
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

func fetch(url, accept string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt < 5; attempt++ {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
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

			if resp.StatusCode != 404 &&
				resp.StatusCode != 429 &&
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

// getInternetDataRecords downloads the InternetData ASN database and returns
// one record per ASN.
func getInternetDataRecords() ([]asnRecord, error) {
	key := strings.TrimSpace(os.Getenv(apiKeyEnv))
	if key == "" {
		return nil, fmt.Errorf("%s is not set", apiKeyEnv)
	}

	body, err := fetch(asnDatabaseURL+"&apikey="+key, "application/octet-stream")
	if err != nil {
		return nil, fmt.Errorf("download ASN database: %w", err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("open ASN database archive: %w", err)
	}
	defer gz.Close()

	reader := csv.NewReader(gz)
	reader.ReuseRecord = true
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read ASN database header: %w", err)
	}

	asnIdx, nameIdx, typeIdx := -1, -1, -1
	for i, col := range header {
		switch strings.TrimSpace(col) {
		case "asn":
			asnIdx = i
		case "name":
			nameIdx = i
		case "type":
			typeIdx = i
		}
	}
	if asnIdx < 0 || typeIdx < 0 {
		return nil, fmt.Errorf("ASN database missing asn/type columns: %v", header)
	}

	var records []asnRecord
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read ASN database row: %w", err)
		}
		if typeIdx >= len(record) || asnIdx >= len(record) {
			continue
		}

		asn, err := strconv.Atoi(strings.TrimSpace(record[asnIdx]))
		if err != nil || asn <= 0 {
			continue
		}

		var name string
		if nameIdx >= 0 && nameIdx < len(record) {
			name = strings.TrimSpace(record[nameIdx])
		}

		records = append(records, asnRecord{
			asn:  asn,
			name: name,
			kind: strings.TrimSpace(record[typeIdx]),
		})
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("ASN database is empty")
	}
	return records, nil
}

func getRecords() ([]asnRecord, error) {
	records, err := getInternetDataRecords()
	if err != nil {
		return nil, err
	}

	fmt.Printf("Source: InternetData (%d ASNs)\n", len(records))
	return records, nil
}

func fileName(kind string) string {
	if mapped, ok := categoryFiles[strings.ToLower(strings.TrimSpace(kind))]; ok {
		return mapped
	}

	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(kind)) {
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

// loadHostingWhitelist reads hostingWhitelist and returns the set of ASNs to be
// excluded from hosting.txt. A missing file is not an error.
func loadHostingWhitelist() (map[int]struct{}, error) {
	file, err := os.Open(hostingWhitelist)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	whitelist := make(map[int]struct{})
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		asn, err := strconv.Atoi(line)
		if err != nil || asn <= 0 {
			fmt.Fprintf(os.Stderr, "whitelist: skipping invalid line %q\n", line)
			continue
		}
		whitelist[asn] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return whitelist, nil
}

func filterLines(lines []string, exclude map[int]struct{}) []string {
	if len(exclude) == 0 {
		return lines
	}

	kept := make([]string, 0, len(lines))
	removed := 0

	for _, line := range lines {
		num, _, _ := strings.Cut(line, " #")
		if asn, err := strconv.Atoi(strings.TrimSpace(num)); err == nil {
			if _, skip := exclude[asn]; skip {
				removed++
				continue
			}
		}
		kept = append(kept, line)
	}

	fmt.Printf("hosting whitelist: excluded %d (of %d)\n", removed, len(lines))
	return kept
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

	records, err := getRecords()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(err)
	}

	whitelist, err := loadHostingWhitelist()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(whitelist) > 0 {
		fmt.Printf("Loaded %d ASNs from %s\n", len(whitelist), hostingWhitelist)
	}

	output := make(map[string][]string)
	for _, r := range records {
		line := strconv.Itoa(r.asn)
		if r.name != "" {
			line += " # " + r.name
		}
		name := fileName(r.kind)
		output[name] = append(output[name], line)
	}

	for name, lines := range output {
		if name == "hosting" {
			output[name] = filterLines(lines, whitelist)
		}
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
