package raft

import (
	"math/rand"
	"time"
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

type RequestVote struct {
	Args  RequestVoteArgs
	Reply RequestVoteReply
}

func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if args.Term < rf.CurrentTerm {
		reply.Term = rf.CurrentTerm
		reply.VoteGranted = false
		return
	}
	if args.Term > rf.CurrentTerm {
		rf.CurrentTerm = args.Term
		rf.Role = Follower
		rf.VotedFor = -1
	}
	lastIdx := len(rf.Log) - 1
	lastTerm := rf.Log[lastIdx].Term
	candidateUptoDate :=
		args.LastLogTerm > lastTerm ||
			(args.LastLogTerm == lastTerm && args.LastLogIndex >= lastIdx)

	if !candidateUptoDate {
		reply.VoteGranted = false
		return
	}
	if rf.VotedFor == -1 || rf.VotedFor == args.CandidateID {
		rf.VotedFor = args.CandidateID
		reply.VoteGranted = true
		rf.LastHeartbeat = time.Now() // reset timer khi grant vote
	} else {
		reply.VoteGranted = false
	}
	reply.Term = rf.CurrentTerm
}

func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) ticker() {
	for rf.killed() == false {
		rf.mu.Lock()
		role := rf.Role
		LastHeartBeat := rf.LastHeartbeat
		rf.mu.Unlock()
		if role == Leader {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		timeout := time.Duration(300+rand.Intn(200)) * time.Millisecond // đúng
		if time.Since(LastHeartBeat) > timeout {
			rf.mu.Lock()
			rf.Role = Candidate
			rf.CurrentTerm += 1
			rf.VotedFor = rf.me
			rf.LastHeartbeat = time.Now()
			Term := rf.CurrentTerm
			VotingCount := 1
			args := RequestVoteArgs{
				Term:         Term,
				CandidateID:  rf.me,
				LastLogIndex: len(rf.Log) - 1,
				LastLogTerm:  rf.Log[len(rf.Log)-1].Term,
			}
			rf.mu.Unlock()
			for i := range rf.peers {
				if i != rf.me {
					go func(server int, args RequestVoteArgs) {
						var reply RequestVoteReply
						ok := rf.sendRequestVote(server, &args, &reply)
						if ok {
							rf.mu.Lock()
							defer rf.mu.Unlock()
							if reply.VoteGranted {
								VotingCount += 1
								if VotingCount > len(rf.peers)/2 && rf.Role == Candidate && rf.CurrentTerm == Term {
									rf.Role = Leader
									// initialize leader volatile state
									rf.NextIndex = make([]int, len(rf.peers))
									rf.MatchIndex = make([]int, len(rf.peers))
									rf.Replicating = make([]bool, len(rf.peers))
									for i := range rf.peers {
										rf.NextIndex[i] = len(rf.Log)
										rf.MatchIndex[i] = 0
										rf.Replicating[i] = false
									}
									rf.MatchIndex[rf.me] = len(rf.Log) - 1
									rf.NextIndex[rf.me] = len(rf.Log)
									go rf.HeartBeatLoop()
									return
								} else {
									return
								}
							} else {
								if reply.Term > rf.CurrentTerm {
									rf.CurrentTerm = reply.Term
									rf.Role = Follower
									rf.VotedFor = -1
									return
								} else {
									return
								}
							}
						}
					}(i, args)
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (rf *Raft) HeartBeatLoop() {
	for rf.killed() == false {
		rf.mu.Lock()
		if rf.Role != Leader {
			rf.mu.Unlock()
			return
		}
		rf.mu.Unlock()
		rf.SendHeartBeat()
		time.Sleep(100 * time.Millisecond)
	}
}

func (rf *Raft) SendHeartBeat() {
	for i := range rf.peers {
		if i != rf.me {
			rf.triggerReplicate(i)
		}
	}
}
