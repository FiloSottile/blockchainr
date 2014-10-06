// Copyright (c) 2013 Conformal Systems LLC.
// Copyright (c) 2014 Filippo Valsorda
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	"github.com/conformal/btcdb"
	_ "github.com/conformal/btcdb/ldb"
	"github.com/conformal/btclog"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
	"github.com/conformal/go-flags"
)

type ShaHash btcwire.ShaHash

type config struct {
	DataDir  string `short:"b" long:"datadir" description:"Directory to store data"`
	DbType   string `long:"dbtype" description:"Database backend"`
	TestNet3 bool   `long:"testnet" description:"Use the test network"`
}

var (
	btcdHomeDir    = btcutil.AppDataDir("btcd", false)
	defaultDataDir = filepath.Join(btcdHomeDir, "data")
	log            btclog.Logger
)

const (
	ArgSha = iota
	ArgHeight
)

func main() {
	cfg := config{
		DbType:  "leveldb",
		DataDir: defaultDataDir,
	}
	parser := flags.NewParser(&cfg, flags.Default)
	args, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			parser.WriteHelp(os.Stderr)
		}
		return
	}
	if len(args) < 1 {
		os.Stderr.WriteString("Please specify the list file")
		return
	}

	backendLogger := btclog.NewDefaultBackendLogger()
	defer backendLogger.Flush()
	log = btclog.NewSubsystemLogger(backendLogger, "")
	btcdb.UseLogger(log)

	var testnet string
	if cfg.TestNet3 {
		testnet = "testnet"
	} else {
		testnet = "mainnet"
	}

	cfg.DataDir = filepath.Join(cfg.DataDir, testnet)

	blockDbNamePrefix := "blocks"
	dbName := blockDbNamePrefix + "_" + cfg.DbType
	if cfg.DbType == "sqlite" {
		dbName = dbName + ".db"
	}
	dbPath := filepath.Join(cfg.DataDir, dbName)

	log.Infof("loading db %v", cfg.DbType)
	db, err := btcdb.OpenDB(cfg.DbType, dbPath)
	if err != nil {
		log.Warnf("db open failed: %v", err)
		return
	}
	defer db.Close()
	log.Infof("db load complete")

	list_file, err := os.Open(args[0])
	if err != nil {
		log.Warnf("file open failed: %v", err)
		return
	}
	list_str := make([]byte, 10*1024)
	read_bytes, err := list_file.Read(list_str)
	if err != nil {
		log.Warnf("file read failed: %v", err)
		return
	}
	list_str = bytes.Trim(list_str[:read_bytes], "\n")
	list := bytes.Split(list_str, []byte(","))

	exported_blocks := make(map[string]string)

	for i := 0; i < len(list); i++ {
		height, err := strconv.ParseInt(string(list[i]), 10, 64)
		if err != nil {
			log.Warnf("ParseInt %v failed: %v", string(list[i]), err)
			continue
		}

		sha, err := db.FetchBlockShaByHeight(height)
		if err != nil {
			log.Warnf("FetchBlockShaByHeight %v failed: %v", string(list[i]), err)
			continue
		}
		blk, err := db.FetchBlockBySha(sha)
		if err != nil {
			log.Warnf("FetchBlockBySha %v failed: %v", string(list[i]), err)
			continue
		}
		bts, err := blk.Bytes()
		if err != nil {
			log.Warnf("Bytes %v failed: %v", string(list[i]), err)
			continue
		}

		exported_blocks[strconv.FormatInt(height, 10)] = hex.EncodeToString(bts)
	}

	resultsFile, err := os.Create("blocks.json")
	if err != nil {
		log.Warnf("failed to create blocks.json: %v", err)
		return
	}
	json_result, err := json.Marshal(exported_blocks)
	if err != nil {
		log.Warnf("failed to Marshal the result: %v", err)
		return
	}
	resultsFile.Write(json_result)
}
