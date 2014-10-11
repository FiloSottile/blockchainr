package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"

	"github.com/conformal/btcdb"
	_ "github.com/conformal/btcdb/ldb"
	"github.com/conformal/btcec"
	"github.com/conformal/btcnet"
	"github.com/conformal/btcscript"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
)

type inData struct {
	H    int64
	Tx   int
	TxIn int
}

type rData struct {
	r  string
	in *inData

	blkSha *btcwire.ShaHash
	blk    *btcutil.Block
	tx     *btcutil.Tx

	txIn      *btcwire.TxIn
	txInIndex int

	txPrev         *btcdb.TxListReply
	txPrevOut      *btcwire.TxOut
	txPrevOutIndex uint32
	blkPrev        *btcutil.Block

	sigStr []byte
	pkStr  []byte

	signature *btcec.Signature
	pubKey    *btcec.PublicKey
	hash      []byte

	address    string
	compressed bool

	wif *btcutil.WIF
}

func btcdbSetup(dataDir, dbType string) (db btcdb.Db, err error) {
	// Setup database access
	blockDbNamePrefix := "blocks"
	dbName := blockDbNamePrefix + "_" + dbType
	if dbType == "sqlite" {
		dbName = dbName + ".db"
	}
	dbPath := filepath.Join(dataDir, "mainnet", dbName)

	db, err = btcdb.OpenDB(dbType, dbPath)

	return
}

func fetch(db btcdb.Db, rd *rData) error {
	sha, err := db.FetchBlockShaByHeight(rd.in.H)
	if err != nil {
		return fmt.Errorf("failed FetchBlockShaByHeight(%v): %v\n", rd.in.H, err)
	}
	blk, err := db.FetchBlockBySha(sha)
	if err != nil {
		return fmt.Errorf("failed FetchBlockBySha(%v) - h %v: %v\n", sha, rd.in.H, err)
	}

	tx := blk.Transactions()[rd.in.Tx]

	rd.blkSha = sha
	rd.blk = blk
	rd.tx = tx
	rd.txInIndex = rd.in.TxIn
	rd.txIn = tx.MsgTx().TxIn[rd.in.TxIn]

	txPrevList, err := db.FetchTxBySha(&rd.txIn.PreviousOutPoint.Hash)
	if err != nil {
		return fmt.Errorf("failed FetchTxBySha(%v) - h %v: %v\n",
			rd.txIn.PreviousOutPoint.Hash, rd.in.H, err)
	}

	if len(txPrevList) != 1 {
		return fmt.Errorf("not single FetchTxBySha(%v) - h %v: %v\n",
			rd.txIn.PreviousOutPoint.Hash, rd.in.H, len(txPrevList))
	}

	blkPrev, err := db.FetchBlockBySha(txPrevList[0].BlkSha)
	if err != nil {
		return fmt.Errorf("failed prev FetchBlockBySha(%v) - h %v: %v\n",
			txPrevList[0].BlkSha, rd.in.H, err)
	}

	rd.txPrev = txPrevList[0]
	rd.txPrevOutIndex = rd.txIn.PreviousOutPoint.Index
	rd.txPrevOut = rd.txPrev.Tx.TxOut[rd.txPrevOutIndex]
	rd.blkPrev = blkPrev

	return nil
}

func printLine(rd *rData) {
	fmt.Printf("%v\t%v\t%v",
		rd.in.H,
		rd.blkSha.String(),
		rd.blk.MsgBlock().Header.Timestamp.Unix(),
	)

	fmt.Printf("\t%v\t%v\t%v", rd.in.Tx, rd.tx.Sha(), rd.in.TxIn)

	fmt.Printf("\t%v\t%v\t%v",
		rd.blkPrev.Height(),
		rd.txPrev.BlkSha.String(),
		rd.blkPrev.MsgBlock().Header.Timestamp.Unix(),
	)

	fmt.Printf("\t%v", rd.r)

	if rd.address != "" {
		fmt.Printf("\t%v", rd.address)
	}

	if rd.wif != nil {
		fmt.Printf("\t%v", rd.wif.String())
	}

	fmt.Print("\n")
}

func main() {
	var (
		dataDir = flag.String("datadir", filepath.Join(btcutil.AppDataDir("btcd", false), "data"), "BTCD: Data directory")
		dbType  = flag.String("dbtype", "leveldb", "BTCD: Database backend")
	)
	flag.Parse()

	db, err := btcdbSetup(*dataDir, *dbType)
	if err != nil {
		log.Println("btcdbSetup error:", err)
		return
	}
	defer db.Close()

	var jsonFile = flag.String("json", "blockchainr.json", "blockchainr output")
	flag.Parse()

	blockchainrFile, err := ioutil.ReadFile(*jsonFile)
	if err != nil {
		log.Println("failed to read blockchainr.json:", err)
		return
	}

	results := make(map[string][]*inData)
	err = json.Unmarshal(blockchainrFile, &results)
	if err != nil {
		log.Println("Unmarshal error:", err)
		return
	}

	fmt.Println("blkH\tblkSha\tblkTime\ttxIndex\ttxSha\ttxInIndex\tprevBlkH\tprevBlkSha\tprevBlkTime\tr\taddr\twif")

	targets := make(map[[2]string][]*rData)

	for r, inDataList := range results {
		for _, in := range inDataList {
			rd := &rData{r: r, in: in}

			if err := fetch(db, rd); err != nil {
				log.Println("Skipping at fetch:", err)
				printLine(rd)
				continue
			}

			switch t := btcscript.GetScriptClass(rd.txPrevOut.PkScript); t {
			case btcscript.PubKeyHashTy:
				if err := processPubKeyHash(db, rd); err != nil {
					log.Println("Skipping at opCheckSig:", err)
					printLine(rd)
					continue
				}
			default:
				log.Println("Unsupported pkScript type:",
					btcscript.ScriptClassToName[t], rd.in)
				printLine(rd)
				continue
			}

			// TODO: group compressed and uncompressed together
			key := [...]string{rd.address, rd.r}
			targets[key] = append(targets[key], rd)
		}
	}

	// Do the magic!
	for _, target := range targets {
		if len(target) < 2 {
			// The r value was reused across different addresses
			// TODO: also this information would be interesting to graph

			for _, rd := range target {
				printLine(rd)
			}

			continue
		}

		a := target[0]
		b := target[1]

		log.Printf("[%v]\n", a.address)
		log.Printf("Repeated r value: %v (%v times)\n", a.r, len(target))

		privKey := recoverKey(a.signature, b.signature, a.hash, b.hash, a.pubKey)
		if privKey == nil {
			log.Print("recoverKey error\n\n")
			continue
		}

		wif, err := btcutil.NewWIF(privKey, &btcnet.MainNetParams, a.compressed)
		if err != nil {
			log.Printf("NewWIF error: %v\n\n", err)
			continue
		}

		for _, rd := range target {
			rd.wif = wif
			printLine(rd)
		}

		log.Printf("%v\n\n", wif.String())
	}
}
