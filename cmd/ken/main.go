// Modifications Copyright 2024 The Kaia Authors
// Modifications Copyright 2018 The klaytn Authors
// Copyright 2016 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.
//
// This file is derived from cmd/geth/main.go (2018/06/04).
// Modified and improved for the klaytn development.
// Modified and improved for the Kaia development.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/kaiachain/kaia/api/debug"
	"github.com/kaiachain/kaia/cmd/utils"
	"github.com/kaiachain/kaia/cmd/utils/nodecmd"
	"github.com/kaiachain/kaia/console"
	"github.com/kaiachain/kaia/log"
	"github.com/urfave/cli/v2"
)

var (
	logger = log.NewModuleLogger(log.CMDKEN)

	// The app that holds all commands and flags.
	app = utils.NewApp(nodecmd.GetGitCommit(), "The command line interface for Kaia Endpoint Node")
)

func init() {
	// Initialize the CLI app and start ken
	app.Action = nodecmd.RunKaiaNode
	app.HideVersion = true // we have a command to print the version
	app.Copyright = "Copyright 2018-2024 The Kaia Authors"
	app.Commands = []*cli.Command{
		// See utils/nodecmd/chaincmd.go:
		nodecmd.InitCommand,
		nodecmd.DumpGenesisCommand,

		// See utils/nodecmd/accountcmd.go
		nodecmd.AccountCommand,

		// See utils/nodecmd/consolecmd.go:
		nodecmd.GetConsoleCommand(utils.KenNodeFlags(), utils.CommonRPCFlags),
		nodecmd.AttachCommand,

		// See utils/nodecmd/versioncmd.go:
		nodecmd.VersionCommand,

		// See utils/nodecmd/dumpconfigcmd.go:
		nodecmd.GetDumpConfigCommand(utils.KenNodeFlags(), utils.CommonRPCFlags),

		// See utils/nodecmd/db_migration.go:
		nodecmd.MigrationCommand,

		// See utils/nodecmd/util.go:
		nodecmd.UtilCommand,

		// See utils/nodecmd/snapshot.go:
		nodecmd.SnapshotCommand,
	}
	sort.Sort(cli.CommandsByName(app.Commands))

	app.Flags = utils.KenAppFlags()

	app.CommandNotFound = nodecmd.CommandNotExist
	app.OnUsageError = nodecmd.OnUsageError
	app.Before = nodecmd.BeforeRunNode
	app.After = func(ctx *cli.Context) error {
		debug.Exit()
		console.Stdin.Close() // Resets terminal mode.
		return nil
	}
}

func main() {
	// Set NodeTypeFlag to en
	utils.NodeTypeFlag.Value = "en"

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var (
	interval = time.Minute
)

func memoryMonitor() {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		<-ticker.C
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		recs := make([]runtime.MemProfileRecord, 10)
		got, ok := runtime.MemProfile(recs, false)
		if !ok {
			continue
		}
		r := recs[:got]
		byFunc := aggregateMemoryByFunc(r)
		top := sortByMemoryUsage(byFunc)
		// top10
		n := 10
		if len(top) < n {
			n = len(top)
		}
		report := buildHeapReport(m, top, n)
		json, err := json.Marshal(report)
		if err != nil {
			continue
		}
		fmt.Println("[MEM]", string(json))
	}
}

type memoryAgg struct {
	name string
	file string
	line int
	byts int64
}

func aggregateMemoryByFunc(recs []runtime.MemProfileRecord) map[string]memoryAgg {
	byFunc := make(map[string]memoryAgg)

	for _, r := range recs {
		inuse := r.InUseBytes()
		if inuse == 0 {
			continue
		}

		stack := r.Stack()
		if len(stack) == 0 {
			key := "<unknown>::<unknown>::0"
			agg := byFunc[key]
			agg.name = "<unknown>"
			agg.file = "<unknown>"
			agg.line = 0
			agg.byts += inuse
			byFunc[key] = agg
			continue
		}

		pc := stack[0]
		if f := runtime.FuncForPC(pc); f != nil {
			file, line := f.FileLine(pc)
			key := fmt.Sprintf("%s::%s::%d", f.Name(), file, line)
			agg := byFunc[key]
			agg.name = f.Name()
			agg.file = file
			agg.line = line
			agg.byts += inuse
			byFunc[key] = agg
		} else {
			key := "<unknown>::<unknown>::0"
			agg := byFunc[key]
			agg.name = "<unknown>"
			agg.file = "<unknown>"
			agg.line = 0
			agg.byts += inuse
			byFunc[key] = agg
		}
	}

	return byFunc
}

func sortByMemoryUsage(byFunc map[string]memoryAgg) []memoryAgg {
	top := make([]memoryAgg, 0, len(byFunc))
	for _, v := range byFunc {
		top = append(top, v)
	}
	sort.Slice(top, func(i, j int) bool {
		return top[i].byts > top[j].byts
	})
	return top
}

type memStatsInfo struct {
	Alloc      int64  `json:"alloc_bytes"`
	AllocHuman string `json:"alloc_human"`
	TotalAlloc int64  `json:"total_alloc_bytes"`
	TotalHuman string `json:"total_alloc_human"`
	Sys        int64  `json:"sys_bytes"`
	SysHuman   string `json:"sys_human"`
	NumGC      uint32 `json:"num_gc"`
}

type memConsumerInfo struct {
	Rank       int    `json:"rank"`
	Function   string `json:"function"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Bytes      int64  `json:"bytes"`
	BytesHuman string `json:"bytes_human"`
}

type heapReport struct {
	Timestamp   string            `json:"timestamp"`
	MemStats    memStatsInfo      `json:"mem_stats"`
	TopN        int               `json:"top_n"`
	TopConsumer []memConsumerInfo `json:"top_consumers"`
	Total       int64             `json:"total_bytes"`
	TotalHuman  string            `json:"total_human"`
}

func buildHeapReport(m runtime.MemStats, top []memoryAgg, n int) heapReport {
	report := heapReport{
		Timestamp: time.Now().Format(time.RFC3339),
		MemStats: memStatsInfo{
			Alloc:      int64(m.Alloc),
			AllocHuman: humanBytes(int64(m.Alloc)),
			TotalAlloc: int64(m.TotalAlloc),
			TotalHuman: humanBytes(int64(m.TotalAlloc)),
			Sys:        int64(m.Sys),
			SysHuman:   humanBytes(int64(m.Sys)),
			NumGC:      m.NumGC,
		},
		TopN:        n,
		TopConsumer: make([]memConsumerInfo, 0, n),
	}

	var total int64
	for i := 0; i < n; i++ {
		report.TopConsumer = append(report.TopConsumer, memConsumerInfo{
			Rank:       i + 1,
			Function:   top[i].name,
			File:       top[i].file,
			Line:       top[i].line,
			Bytes:      top[i].byts,
			BytesHuman: humanBytes(top[i].byts),
		})
		total += top[i].byts
	}

	report.Total = total
	report.TotalHuman = humanBytes(total)

	return report
}

// humanBytes formats bytes to human-readable format
func humanBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.2fGiB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.2fMiB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.2fKiB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
