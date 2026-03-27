package raft

import (
	"sync"
	"sync/atomic"
	"time"
)

type Peer interface {
	Call(method string, args, reply interface{}) bool
}

// Persister abstracts durable storage for Raft state.
type Persister interface {
	Save(raftState []byte, snapshot []byte)
	ReadRaftState() []byte
	RaftStateSize() int
}

// ApplyMsg is sent on ApplyCh each time a log entry is committed.
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

type RequestVoteArgs struct {
	Term         int
	LastLogIndex int
	LastLogTerm  int
	CandidateID  int
}

type RequestVoteReply struct {
	Term        int
	VoteGranted bool
}

type AppendEntriesArgs struct {
	Term         int        // leader's term
	LeaderID     int        // so follower can redirect clients
	PrevLogIndex int        // index of log entry immediately preceding new ones
	PrevLogTerm  int        // term of prevLogIndex entry
	Entries      []LogEntry // log entries to store (empty for heartbeat; may send more than one for efficiency)
	LeaderCommit int        // leader's commitIndex
}

type AppendEntriesReply struct {
	Term    int  // currentTerm, for leader to update itself
	Success bool // true if follower contained entry matching prevLogIndex and prevLogTerm
	XTerm   int
	XLen    int
	Xindex  int
}

type AppendEntries struct {
	Args  AppendEntriesArgs
	Reply AppendEntriesReply
}

type RequestVote struct {
	Args  RequestVoteArgs
	Reply RequestVoteReply
}

type Raft struct {
	mu        sync.Mutex
	peers     []Peer
	persister Persister
	me        int
	dead      int32

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
	LastHeartbeat time.Time // để check election timeout
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

func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// TODO (3D)
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
	rf.persist()
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
	rf.Log = []LogEntry{{Term: 0}} // dummy entry index 0
	rf.CommitIndex = 0
	rf.LastApplied = 0
	rf.ApplyCh = applyCh
	rf.Replicating = make([]bool, len(peers))

	rf.readPersist(persister.ReadRaftState())

	go rf.ticker()
	go rf.applyLoop()

	return rf
}
