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
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	source  = "https://raw.githubusercontent.com/ipverse/as-metadata/master/as.csv"
	api     = "https://asn.ipinfo.app/api/json/details/AS%d"
	workers = 3000
)

var (
	kindRE = regexp.MustCompile(`"kind"\s*:\s*(?:"([^"]*)"|(null))`)
	nameRE = regexp.MustCompile(`"name"\s*:\s*("(?:\\.|[^"\\])*")`)
	client *http.Client
)

type result struct {
	asn    int
	kind   string
	line   string
	retry  bool
	failed bool
}

func classify(asn int) result {
	body, err := fetch(fmt.Sprintf(api, asn))
	if err != nil {
		if errors.Is(err, errNotFound) {
			return result{asn: asn, failed: true}
		}
		return result{asn: asn, retry: true}
	}

	km := kindRE.FindSubmatch(body)
	nm := nameRE.FindSubmatch(body)
	if len(km) == 0 || len(nm) == 0 {
		return result{asn: asn, retry: true}
	}

	var name string
	if err := json.Unmarshal(nm[1], &name); err != nil {
		return result{asn: asn, retry: true}
	}

	var kind string
	if string(km[1]) != "" {
		kind = string(km[1])
	} else if string(km[2]) == "null" {
		kind = "unknown"
	} else {
		return result{asn: asn, retry: true}
	}

	switch kind {
	case "hosting", "residential", "unknown":
		return result{
			asn:  asn,
			kind: kind,
			line: fmt.Sprintf("%d # %s", asn, name),
		}
	default:
		return result{asn: asn, retry: true}
	}
}

func run(asns []int) []result {
	jobs := make(chan int, workers*2)
	results := make(chan result, workers*2)

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for asn := range jobs {
				results <- classify(asn)
			}
		}()
	}

	go func() {
		for _, asn := range asns {
			jobs <- asn
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	out := make([]result, 0, len(asns))
	for r := range results {
		out = append(out, r)
	}
	return out
}

func init() {
	client = &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          1000,
			MaxIdleConnsPerHost:   500,
			MaxConnsPerHost:       500,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
}

var errNotFound = fmt.Errorf("not found")

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

			if resp.StatusCode == http.StatusNotFound {
				return nil, errNotFound
			}

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

			if resp.StatusCode == 429 {
				if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
					if seconds, err := strconv.Atoi(retryAfter); err == nil &&
						seconds > 0 && seconds <= 60 {
						time.Sleep(time.Duration(seconds) * time.Second)
						continue
					}
				}
			}
		}

		if attempt < 4 {
			time.Sleep(time.Duration(1<<attempt) * time.Second)
		}
	}

	return nil, lastErr
}

func getASNs() ([]int, error) {
	body, err := fetch(source)
	if err != nil {
		return nil, fmt.Errorf("download ASN list: %w", err)
	}

	reader := csv.NewReader(bytes.NewReader(body))
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return nil, err
	}

	asnCol := -1
	for i, col := range header {
		if strings.EqualFold(strings.TrimSpace(col), "asn") {
			asnCol = i
			break
		}
	}
	if asnCol < 0 {
		return nil, fmt.Errorf("ASN column not found; headers: %v", header)
	}

	seen := make(map[int]struct{})

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if asnCol >= len(record) {
			continue
		}

		value := strings.TrimSpace(record[asnCol])
		value = strings.TrimPrefix(strings.ToUpper(value), "AS")

		asn, err := strconv.Atoi(value)
		if err != nil || asn <= 0 {
			continue
		}
		seen[asn] = struct{}{}
	}

	asns := make([]int, 0, len(seen))
	for asn := range seen {
		asns = append(asns, asn)
	}
	sort.Ints(asns)

	return asns, nil
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

	asns, err := getASNs()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("Found %d ASNs; workers=%d\n", len(asns), workers)

	if err := os.MkdirAll("data", 0755); err != nil {
		panic(err)
	}

	output := map[string][]string{
		"hosting":     {},
		"residential": {},
		"unknown":     {},
	}
	var failed []int

	collect := func(rs []result) {
		for _, r := range rs {
			switch {
			case r.kind != "":
				output[r.kind] = append(output[r.kind], r.line)
			case r.failed:
				failed = append(failed, r.asn)
			}
		}
	}

	first := run(asns)
	collect(first)
	fmt.Printf("Processed %d/%d (first pass)\n", len(first), len(asns))

	var retry []int
	for _, r := range first {
		if r.retry {
			retry = append(retry, r.asn)
		}
	}

	if len(retry) > 0 {
		fmt.Printf("Retrying %d ASNs...\n", len(retry))
		second := run(retry)
		collect(second)
		for _, r := range second {
			if r.retry {
				failed = append(failed, r.asn)
			}
		}
	}

	fmt.Printf("Elapsed: %s\n", time.Since(start).Round(time.Second))

	for kind, lines := range output {
		path := "data/" + kind + ".txt"
		if err := writeLines(path, lines); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%s: %d entries\n", path, len(lines))
	}

	sort.Ints(failed)
	file, err := os.Create("data/failed.txt")
	if err != nil {
		panic(err)
	}
	for _, asn := range failed {
		fmt.Fprintln(file, asn)
	}
	file.Close()

	fmt.Printf("Failed/unclassified: %d\n", len(failed))
}
