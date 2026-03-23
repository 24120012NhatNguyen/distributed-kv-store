package raft

import (
	"sync"
	"sync/atomic"
	"time"
)

type Peer interface {
	Call(method string, args, reply interface{}) bool
}

type Persister interface {
	Save(raftState []byte, snapshot []byte)
	ReadRaftState() []byte
	RaftStateSize() int
}

type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

type LogEntry struct {
	Command  interface{}
	Term     int
	LogIndex int
}

type Role int

const (
	Follower Role = iota
	Candidate
	Leader
)

type Raft struct {
	mu        sync.Mutex
	peers     []Peer
	persister Persister
	me        int
	dead      int32

	// Persistent state
	CurrentTerm int
	VotedFor    int
	Log         []LogEntry

	// Volatile state
	CommitIndex int
	LastApplied int

	// Leader-only volatile state
	NextIndex  []int
	MatchIndex []int
	// Prevent concurrent (duplicate) replication loops per follower.
	Replicating []bool

	// Extra
	Role          Role
	LastHeartbeat time.Time // check election timeout
	ApplyCh       chan ApplyMsg
}

func (rf *Raft) GetState() (int, bool) {
	var term int
	var isleader bool
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = rf.CurrentTerm
	isleader = rf.Role == Leader
	return term, isleader
}

func (rf *Raft) persist() {
}

func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 {
		return
	}
}

func (rf *Raft) PersistBytes() int {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.persister.RaftStateSize()
}

func (rf *Raft) Snapshot(index int, snapshot []byte) {
}

func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true
	rf.mu.Lock()
	if rf.Role != Leader {
		isLeader = false
		rf.mu.Unlock()
		return index, term, isLeader
	}
	term = rf.CurrentTerm
	index = len(rf.Log)
	rf.Log = append(rf.Log, LogEntry{
		Command:  command,
		Term:     rf.CurrentTerm,
		LogIndex: index,
	})
	index = len(rf.Log) - 1
	// Keep leader's own indices consistent.
	if rf.MatchIndex != nil && len(rf.MatchIndex) > rf.me {
		rf.MatchIndex[rf.me] = index
	}
	if rf.NextIndex != nil && len(rf.NextIndex) > rf.me {
		rf.NextIndex[rf.me] = index + 1
	}
	rf.mu.Unlock()

	// Kick off replication immediately; don't wait for the next heartbeat.
	for i := range rf.peers {
		if i != rf.me {
			rf.triggerReplicate(i)
		}
	}
	return index, term, isLeader
}

func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

func Make(peers []Peer, me int, persister Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	rf.Role = Follower
	rf.CurrentTerm = 0
	rf.VotedFor = -1
	rf.LastHeartbeat = time.Now()
	rf.Log = []LogEntry{{Term: 0}}
	rf.CommitIndex = 0
	rf.LastApplied = 0
	rf.ApplyCh = applyCh
	rf.Replicating = make([]bool, len(peers))

	rf.readPersist(persister.ReadRaftState())

	go rf.ticker()
	go rf.applyLoop()

	return rf
}
