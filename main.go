package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"github.com/gorilla/mux"
	"github.com/mediocregopher/radix.v2/redis"
	"github.com/mediocregopher/radix.v2/pool"
)

var (
	p *pool.Pool
)

const (
	K = 32
	initialElo = 1600.0
)

type Match struct {
	Winner string `json:"winner"`
	Loser  string `json:"loser"`
}

func PlayerHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := p.Get()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Could not connect to redis")
		return
	}
	defer p.Put(conn)

	vars := mux.Vars(r)
	player := vars["player"]
	conn.Cmd("HSET", player, initialElo)
}

func MatchHandler(w http.ResponseWriter, r *http.Request) {
	var m Match
	err := json.NewDecoder(r.Body).Decode(&m)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Could not decode json body")
		return
	}

	conn, err := p.Get()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Could not connect to redis")
		return
	}
	defer p.Put(conn)
	elos := allElos(conn)
	if elos == nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Could not retrieve elos from redis")
		return
	}

	winnerElo, found := elos[m.Winner]
	if !found {
		winnerElo = initialElo
	}
	loserElo, found := elos[m.Loser]
	if !found {
		loserElo = initialElo
	}

	rW := math.Pow(10, winnerElo/400.0)
	rL := math.Pow(10, loserElo/400.0)
	eW := rW / (rW + rL)
	eL := rL / (rW + rL)
	winnerElo = winnerElo + K * (1 - eW)
	loserElo = loserElo - K * eL

	winnerResp := conn.Cmd("HSET", "elo", m.Winner, winnerElo)
	loserResp := conn.Cmd("HSET", "elo", m.Loser, loserElo)
	if winnerResp.Err != nil || loserResp.Err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Could not update ELO stats")
		return
	}
}

func allElos(conn *redis.Client) map[string]float64 {
	result := conn.Cmd("HGETALL", "elo")
	if result.Err != nil {
		return nil
	}

	elos, err := result.Map()
	if err != nil {
		return nil
	}

	retval := make(map[string]float64)
	for player, elo := range(elos) {
		f, err := strconv.ParseFloat(elo, 64)
		if err != nil {
			conn.Cmd("HDEL", "elo", player)
		} else {
			retval[player] = f
		}
	}

	return retval
}

func EloHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := p.Get()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Could not connect to redis")
		return
	}
	defer p.Put(conn)

	vars := mux.Vars(r)
	player, found := vars["player"]

	elos := allElos(conn)
	if elos == nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Could not retrieve elos from redis")
		return
	}

	result := make(map[string]int)
	if found {
		elo, exists := elos[player]
		if exists {
			result[player] = int(elo)
		} else {
			result[player] = initialElo
		}
	} else {
		for player, elo := range(elos) {
			result[player] = int(elo)
		}
	}
	
	json.NewEncoder(w).Encode(result)
}

func main() {
	var err error
	p, err = pool.New("tcp", "localhost:6379", 10)
	if err != nil {
		log.Fatalln("Could not connect to redis, exiting...")
	}

	r := mux.NewRouter()
	r.HandleFunc("/match", MatchHandler).Methods("POST")
	r.HandleFunc("/new/{player}", PlayerHandler).Methods("POST")
	r.HandleFunc("/elo", EloHandler).Methods("GET")
	r.HandleFunc("/elo/{player}", EloHandler).Methods("GET")
	fmt.Println("starting.")
	log.Fatal(http.ListenAndServe(":8080", r))
	fmt.Println("stopping.")
}
