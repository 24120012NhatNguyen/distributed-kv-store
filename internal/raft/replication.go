package raft

import (
	"time"
)

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
}

type AppendEntries struct {
	Args  AppendEntriesArgs
	Reply AppendEntriesReply
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// Rule 1: Reply false if term < currentTerm
	if args.Term < rf.CurrentTerm {
		reply.Term = rf.CurrentTerm
		reply.Success = false
		return
	}
	if args.Term > rf.CurrentTerm {
		rf.VotedFor = -1
	}
	rf.CurrentTerm = args.Term
	rf.Role = Follower
	rf.LastHeartbeat = time.Now() // reset timer khi nhận được AppendEntries từ leader
	reply.Term = rf.CurrentTerm
	// Rule 2: Consistency check - prevLogIndex and prevLogTerm must match
	if args.PrevLogIndex >= len(rf.Log) || rf.Log[args.PrevLogIndex].Term != args.PrevLogTerm {
		reply.Success = false
		return
	}
	// Rules 3 & 4: Truncate and append new entries
	for i, entry := range args.Entries {
		idx := args.PrevLogIndex + 1 + i
		if idx < len(rf.Log) {
			// ghi đè lên
			if rf.Log[idx].Term != entry.Term {
				rf.Log = rf.Log[:idx]                        //  index [0, idx-1]
				rf.Log = append(rf.Log, args.Entries[i:]...) // thay thế
				break
			}
		} else {
			rf.Log = append(rf.Log, args.Entries[i:]...)
			break
		}
	}
	if args.LeaderCommit > rf.CommitIndex {
		rf.CommitIndex = min(args.LeaderCommit, len(rf.Log)-1)
	}
	reply.Success = true
}

func (rf *Raft) triggerReplicate(server int) {
	rf.mu.Lock()
	if rf.Role != Leader {
		rf.mu.Unlock()
		return
	}
	if server == rf.me {
		rf.mu.Unlock()
		return
	}
	if rf.Replicating[server] {
		rf.mu.Unlock()
		return
	}
	rf.Replicating[server] = true
	rf.mu.Unlock()

	go func() {
		rf.replicateToFollower(server)
		rf.mu.Lock()
		rf.Replicating[server] = false
		rf.mu.Unlock()
	}()
}

func (rf *Raft) replicateToFollower(server int) {
	for rf.killed() == false {
		rf.mu.Lock()
		if rf.Role != Leader {
			rf.mu.Unlock()
			return
		}
		nextIndex := rf.NextIndex[server]
		if nextIndex < 1 {
			nextIndex = 1
			rf.NextIndex[server] = 1
		}
		if nextIndex > len(rf.Log) {
			nextIndex = len(rf.Log)
			rf.NextIndex[server] = len(rf.Log)
		}
		prevIdx := nextIndex - 1
		prevTerm := rf.Log[prevIdx].Term
		entries := make([]LogEntry, len(rf.Log[nextIndex:]))
		copy(entries, rf.Log[nextIndex:])
		args := AppendEntriesArgs{
			Term:         rf.CurrentTerm,
			LeaderID:     rf.me,
			PrevLogIndex: prevIdx,
			PrevLogTerm:  prevTerm,
			Entries:      entries,
			LeaderCommit: rf.CommitIndex,
		}
		term := rf.CurrentTerm
		rf.mu.Unlock()

		var reply AppendEntriesReply
		if !rf.peers[server].Call("Raft.AppendEntries", &args, &reply) {
			return
		}

		rf.mu.Lock()
		if reply.Term > rf.CurrentTerm {
			rf.CurrentTerm = reply.Term
			rf.Role = Follower
			rf.VotedFor = -1
			rf.LastHeartbeat = time.Now()
			rf.mu.Unlock()
			return
		}
		if rf.Role != Leader || rf.CurrentTerm != term {
			rf.mu.Unlock()
			return
		}
		if reply.Success {
			match := args.PrevLogIndex + len(args.Entries)
			rf.MatchIndex[server] = match
			rf.NextIndex[server] = match + 1
			rf.maybeCommit()
			rf.mu.Unlock()
			return
		}
		if rf.NextIndex[server] > 1 {
			rf.NextIndex[server] -= 1
		}
		rf.mu.Unlock()
		// Fast backup over inconsistent follower logs, without waiting for the next heartbeat.
		time.Sleep(5 * time.Millisecond)
	}
}

func (rf *Raft) maybeCommit() {
	for N := len(rf.Log) - 1; N > rf.CommitIndex; N-- {
		if rf.Log[N].Term != rf.CurrentTerm {
			continue
		}
		count := 1
		for i := range rf.peers {
			if i != rf.me && rf.MatchIndex[i] >= N {
				count += 1
			}
		}
		if count > len(rf.peers)/2 {
			rf.CommitIndex = N
			break
		}
	}
}

func (rf *Raft) applyLoop() {
	for rf.killed() == false {
		time.Sleep(10 * time.Millisecond)
		rf.mu.Lock()
		entries := []ApplyMsg{}
		for rf.LastApplied < rf.CommitIndex {
			rf.LastApplied += 1
			entries = append(entries, ApplyMsg{
				CommandValid: true,
				Command:      rf.Log[rf.LastApplied].Command,
				CommandIndex: rf.LastApplied,
			})
		}
		rf.mu.Unlock()
		for _, entry := range entries {
			rf.ApplyCh <- entry
		}
	}
}
