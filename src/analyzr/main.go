package main

import (
	"crypto/ecdsa"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"

	"github.com/conformal/btcdb"
	_ "github.com/conformal/btcdb/ldb"
	"github.com/conformal/btcec"
	"github.com/conformal/btcnet"
	"github.com/conformal/btcscript"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
)

func btcdbSetup(dataDir, dbType string) (db btcdb.Db) {
	// Setup database access
	blockDbNamePrefix := "blocks"
	dbName := blockDbNamePrefix + "_" + dbType
	if dbType == "sqlite" {
		dbName = dbName + ".db"
	}
	dbPath := filepath.Join(dataDir, "mainnet", dbName)

	log.Println("loading db", dbType)
	db, err := btcdb.OpenDB(dbType, dbPath)
	if err != nil {
		log.Fatalln("db open failed:", err)
	}
	log.Println("db load complete")

	return
}

var balanceCache = make(map[string][]byte)

func getBalance(address string) string {
	balance, ok := balanceCache[address]

	if ok {
		return string(balance)
	}

	response, err := http.Get("https://blockchain.info/q/addressfirstseen/" + address)
	if err != nil {
		log.Fatalln("Get failed:", err)
	}
	defer response.Body.Close()
	balance, err = ioutil.ReadAll(response.Body)
	if err != nil {
		log.Fatalln("ReadAll failed:", err)
	}
	if string(balance) == "0" {
		balanceCache[address] = []byte("NOT EXISTING")
		return "NOT EXISTING"
	}

	response, err = http.Get("https://blockchain.info/q/addressbalance/" + address)
	if err != nil {
		log.Fatalln("Get failed:", err)
	}
	defer response.Body.Close()
	balance, err = ioutil.ReadAll(response.Body)
	if err != nil {
		log.Fatalln("ReadAll failed:", err)
	}
	balanceCache[address] = balance

	return string(balance)
}

type inData struct {
	H    int64
	Tx   int
	TxIn int
}

type rData struct {
	r  string
	in *inData

	tx *btcutil.Tx

	txIn      *btcwire.TxIn
	txInIndex int

	txPrev         *btcdb.TxListReply
	txPrevOut      *btcwire.TxOut
	txPrevOutIndex uint32

	script *btcscript.Script

	sigStr []byte
	pkStr  []byte

	signature *btcec.Signature
	pubKey    *btcec.PublicKey
	hash      []byte

	address    string
	compressed bool
}

func fetchTx(db btcdb.Db, rd *rData) *rData {
	sha, err := db.FetchBlockShaByHeight(rd.in.H)
	if err != nil {
		log.Printf("failed FetchBlockShaByHeight(%v): %v\n", rd.in.H, err)
		return nil
	}
	blk, err := db.FetchBlockBySha(sha)
	if err != nil {
		log.Printf("failed FetchBlockBySha(%v) - h %v: %v\n", sha, rd.in.H, err)
		return nil
	}

	tx := blk.Transactions()[rd.in.Tx]

	rd.tx = tx
	rd.txInIndex = rd.in.TxIn
	rd.txIn = tx.MsgTx().TxIn[rd.in.TxIn]

	return rd
}

func fetchPrev(db btcdb.Db, rd *rData) *rData {
	txPrevList, err := db.FetchTxBySha(&rd.txIn.PreviousOutPoint.Hash)
	if err != nil {
		log.Printf("failed FetchTxBySha(%v) - h %v: %v\n",
			rd.txIn.PreviousOutPoint.Hash, rd.in.H, err)
		return nil
	}

	if len(txPrevList) != 1 {
		log.Printf("not single FetchTxBySha(%v) - h %v: %v\n",
			rd.txIn.PreviousOutPoint.Hash, rd.in.H, len(txPrevList))
		return nil
	}

	rd.txPrev = txPrevList[0]
	rd.txPrevOutIndex = rd.txIn.PreviousOutPoint.Index
	rd.txPrevOut = rd.txPrev.Tx.TxOut[rd.txPrevOutIndex]

	return rd
}

func initEngine(db btcdb.Db, rd *rData) *rData {
	sigScript := rd.txIn.SignatureScript
	pkScript := rd.txPrevOut.PkScript
	script, err := btcscript.NewScript(sigScript, pkScript, rd.txInIndex, rd.tx.MsgTx(), 0)
	if err != nil {
		log.Printf("failed btcscript.NewScript - h %v: %v\n", rd.in.H, err)
		return nil
	}

	if btcscript.GetScriptClass(pkScript) != btcscript.PubKeyHashTy {
		log.Printf("Not a PubKeyHash - in %v\n", rd.in)
		return nil
	}

	// err = script.Execute()
	// log.Println(in, err)
	// os.Exit(0)

	for i := 0; i < 6; i++ {
		_, err := script.Step()
		if err != nil {
			log.Printf("Failed Step - in %v: %v\n", rd.in, err)
			return nil
		}
	}

	data := script.GetStack()

	rd.sigStr = data[0]
	rd.pkStr = data[1]
	rd.script = script

	aPubKey, err := btcutil.NewAddressPubKey(rd.pkStr, &btcnet.MainNetParams)
	if err != nil {
		log.Println("Pubkey parse error:", err)
		return nil
	}
	rd.address = aPubKey.EncodeAddress()
	rd.compressed = aPubKey.Format() == btcutil.PKFCompressed

	return rd
}

func opCheckSig(db btcdb.Db, rd *rData) *rData {
	// From github.com/conformal/btcscript/opcode.go

	// Signature actually needs needs to be longer than this, but we need
	// at least  1 byte for the below. btcec will check full length upon
	// parsing the signature.
	if len(rd.sigStr) < 1 {
		log.Print("OP_CHECKSIG ERROR")
		return nil
	}

	// Trim off hashtype from the signature string.
	hashType := rd.sigStr[len(rd.sigStr)-1]
	sigStr := rd.sigStr[:len(rd.sigStr)-1]

	// Get script from the last OP_CODESEPARATOR and without any subsequent
	// OP_CODESEPARATORs
	subScript := rd.script.SubScript()

	// Unlikely to hit any cases here, but remove the signature from
	// the script if present.
	subScript = btcscript.RemoveOpcodeByData(subScript, sigStr)

	hash := btcscript.CalcScriptHash(subScript, hashType, rd.tx.MsgTx(), rd.txInIndex)

	pubKey, err := btcec.ParsePubKey(rd.pkStr, btcec.S256())
	if err != nil {
		log.Print("OP_CHECKSIG ERROR")
		return nil
	}

	signature, err := btcec.ParseSignature(sigStr, btcec.S256())
	if err != nil {
		log.Print("OP_CHECKSIG ERROR")
		return nil
	}

	// log.Printf("op_checksig\n"+
	// 	"pubKey:\n%v"+
	// 	"pubKey.X: %v\n"+
	// 	"pubKey.Y: %v\n"+
	// 	"signature.R: %v\n"+
	// 	"signature.S: %v\n"+
	// 	"checkScriptHash:\n%v",
	// 	hex.Dump(pkStr), pubKey.X, pubKey.Y,
	// 	signature.R, signature.S, hex.Dump(hash))

	if ok := ecdsa.Verify(pubKey.ToECDSA(), hash, signature.R, signature.S); !ok {
		log.Print("OP_CHECKSIG FAIL")
		return nil
	}

	rd.signature = signature
	rd.pubKey = pubKey
	rd.hash = hash

	return rd
}

func main() {
	var (
		dataDir = flag.String("datadir", filepath.Join(btcutil.AppDataDir("btcd", false), "data"), "BTCD: Data directory")
		dbType  = flag.String("dbtype", "leveldb", "BTCD: Database backend")
	)
	flag.Parse()

	db := btcdbSetup(*dataDir, *dbType)
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

	targets := make(map[[2]string][]*rData)

	for r, inDataList := range results {
		// log.Println(r)

	inLoop:
		for _, in := range inDataList {
			rd := &rData{r: r, in: in}

			for i, f := range []func(btcdb.Db, *rData) *rData{
				fetchTx, fetchPrev, initEngine, opCheckSig,
			} {
				rd = f(db, rd)
				if rd == nil {
					log.Println("Skipping at stage", i)
					continue inLoop
				}
			}

			// TODO: group compressed and uncompressed together
			key := [...]string{rd.address, rd.r}
			targets[key] = append(targets[key], rd)
		}
	}

	doTheMagic(targets)
}
