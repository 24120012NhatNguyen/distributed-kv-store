package kvsrv

import (
	"time"
)

const retryInterval = 100 * time.Millisecond


type IKVClerk interface {
	Get(key string) (string, Tversion, Err)
	Put(key, value string, version Tversion) Err
}

type Transport interface {
	Call(method string, args, reply interface{}) bool
}

type Clerk struct {
	transport Transport
}

func NewClerk(t Transport) *Clerk {
	return &Clerk{transport: t}
}

func (ck *Clerk) Get(key string) (string, Tversion, Err) {
	args := GetArgs{Key: key}
	for {
		reply := GetReply{}
		if ck.transport.Call("KVServer.Get", &args, &reply) {
			return reply.Value, reply.Version, reply.Err
		}
		time.Sleep(retryInterval)
	}
}


func (ck *Clerk) Put(key, value string, version Tversion) Err {
	args := PutArgs{Key: key, Value: value, Version: version}
	retried := false
	for {
		reply := PutReply{}
		if ck.transport.Call("KVServer.Put", &args, &reply) {
			if reply.Err == ErrVersion && retried {
				return ErrMaybe
			}
			return reply.Err
		}
		retried = true
		time.Sleep(retryInterval)
	}
}