package kvsrv

import (
	"log"
	"sync"
)

const debug = false

func dprintf(format string, a ...interface{}) {
	if debug {
		log.Printf("[kvsrv] "+format, a...)
	}
}

// Tversion is the version number of a key.
// Version starts at 1 after the first Put and increments on each successful Put.
type Tversion uint64

type Err string

const (
	OK         Err = "OK"
	ErrNoKey   Err = "ErrNoKey"
	ErrVersion Err = "ErrVersion"
	ErrMaybe   Err = "ErrMaybe"
)

type GetArgs struct {
	Key string
}

type GetReply struct {
	Value   string
	Version Tversion
	Err     Err
}

type PutArgs struct {
	Key     string
	Value   string
	Version Tversion
}

type PutReply struct {
	Err Err
}

type valueVersionPair struct {
	value   string
	version Tversion
}

type KVServer struct {
	mu   sync.Mutex
	data map[string]valueVersionPair
}

func NewKVServer() *KVServer {
	return &KVServer{
		data: make(map[string]valueVersionPair),
	}
}

func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	pair, ok := kv.data[args.Key]
	if !ok {
		reply.Err = ErrNoKey
		return
	}

	reply.Value = pair.value
	reply.Version = pair.version
	reply.Err = OK
}

func (kv *KVServer) Put(args *PutArgs, reply *PutReply) {
	kv.mu.Lock()
	defer kv.mu.Unlock()

	pair, exists := kv.data[args.Key]

	if !exists {
		// New key: version must be 0
		if args.Version != 0 {
			reply.Err = ErrNoKey
			return
		}
		kv.data[args.Key] = valueVersionPair{
			value:   args.Value,
			version: 1,
		}
		reply.Err = OK
		return
	}

	// Existing key: version must match
	if pair.version != args.Version {
		reply.Err = ErrVersion
		return
	}

	kv.data[args.Key] = valueVersionPair{
		value:   args.Value,
		version: pair.version + 1,
	}
	reply.Err = OK
}
