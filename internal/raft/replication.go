package raft

import (
	"time"
)

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// Rule 1: Reply false if term < currentTerm
	if args.Term < rf.CurrentTerm {
		reply.Term = rf.CurrentTerm
		reply.Success = false
		rf.persist()
		return
	}
	if args.Term > rf.CurrentTerm {
		rf.VotedFor = -1
	}
	rf.CurrentTerm = args.Term
	rf.Role = Follower
	rf.LastHeartbeat = time.Now() // reset timer khi nhận được AppendEntries từ leader
	reply.Term = rf.CurrentTerm
	rf.persist()
	// Rule 2: Consistency check - prevLogIndex and prevLogTerm must match
	if args.PrevLogIndex >= len(rf.Log) {
		reply.Success = false
		reply.XTerm = -1
		reply.XLen = len(rf.Log)
		reply.Xindex = -1
		return
	}
	if rf.Log[args.PrevLogIndex].Term != args.PrevLogTerm {
		reply.Success = false
		reply.XTerm = rf.Log[args.PrevLogIndex].Term
		reply.XLen = len(rf.Log)
		xidx := args.PrevLogIndex
		for xidx > 0 && rf.Log[xidx-1].Term == reply.XTerm {
			xidx--
		}
		reply.Xindex = xidx
		return
	}
	// Rules 3 & 4: Truncate and append new entries
	for i, entry := range args.Entries {
		idx := args.PrevLogIndex + 1 + i
		if idx < len(rf.Log) {
			// ghi đè lên
			if rf.Log[idx].Term != entry.Term {
				rf.Log = rf.Log[:idx]                        // giữ lại index [0, idx-1]
				rf.Log = append(rf.Log, args.Entries[i:]...) // thay thế phần còn lại của log
				rf.persist()
				break
			}
		} else {
			rf.Log = append(rf.Log, args.Entries[i:]...)
			rf.persist()
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
		type aeResult struct {
			ok    bool
			reply AppendEntriesReply
		}
		ch := make(chan aeResult, 1)
		go func(a AppendEntriesArgs) {
			var r AppendEntriesReply
			ok := rf.peers[server].Call("Raft.AppendEntries", &a, &r)
			ch <- aeResult{ok: ok, reply: r}
		}(args)
		select {
		case res := <-ch:
			if !res.ok {
				// Unreliable network: retry later instead of giving up.
				time.Sleep(10 * time.Millisecond)
				continue
			}
			reply = res.reply
		case <-time.After(200 * time.Millisecond):
			// Long reordering can delay RPCs; don't let one blocked call stall replication.
			continue
		}

		rf.mu.Lock()
		if reply.Term > rf.CurrentTerm {
			rf.CurrentTerm = reply.Term
			rf.Role = Follower
			rf.VotedFor = -1
			rf.LastHeartbeat = time.Now()
			rf.persist()
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
		// Conflict optimization: jump NextIndex back quickly.
		if reply.XTerm == -1 {
			rf.NextIndex[server] = reply.XLen
		} else {
			lastIndexOfTerm := -1
			for i := len(rf.Log) - 1; i >= 0; i-- {
				if rf.Log[i].Term == reply.XTerm {
					lastIndexOfTerm = i
					break
				}
			}
			if lastIndexOfTerm != -1 {
				rf.NextIndex[server] = lastIndexOfTerm + 1
			} else {
				rf.NextIndex[server] = reply.Xindex
			}
		}
		if rf.NextIndex[server] < 1 {
			rf.NextIndex[server] = 1
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
		if rf.killed() {
			return
		}
		for _, entry := range entries {
			if rf.killed() {
				return
			}
			rf.ApplyCh <- entry
		}
	}
}
