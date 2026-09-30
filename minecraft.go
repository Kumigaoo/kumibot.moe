package main

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"time"
)
const minecraftAddress = "nuestro.kumigaoo.moe:25565"


type MinecraftEvent struct {
	Event       string   `json:"event"`
	Player      string   `json:"player"`
	UUID        string   `json:"uuid"`
	Message     string   `json:"message"`
	Players     []string `json:"players"`
	PlayerCount int      `json:"player_count"`
	MaxPlayers  int      `json:"max_players"`
}
func minecraftHandler(w http.ResponseWriter, r *http.Request) {
	minecraftToken := os.Getenv("MINECRAFT_TOKEN")
	
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	auth := r.Header.Get("Authorization")
	expected := "Bearer " + minecraftToken

	if auth != expected {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var event MinecraftEvent

	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(&event); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	handleMinecraftEvent(event)

	w.WriteHeader(http.StatusNoContent)
}

func handleMinecraftEvent(event MinecraftEvent) {
	switch event.Event {

	case "player_join":
		handleMinecraftJoin(event)

	case "player_quit":
		handleMinecraftQuit(event)

	case "player_death":
		handleMinecraftDeath(event)

	case "chat":
		handleMinecraftChat(event)
	}
}
var (
	lastMinecraftHeartbeat time.Time
)
func checkMinecraft() bool {
    conn, err := net.DialTimeout("tcp", minecraftAddress, 5*time.Second)
    if err != nil {
        return false
    }

    conn.Close()
    return true
}
func updateMinecraftStatus() {
    online := checkMinecraft()

    state.Lock()
    previous := state.MinecraftOnline
    state.MinecraftOnline = online

    if !online {
        state.Players = make(map[string]bool)
    }

    state.Unlock()

    // Offline -> Online
    if online && !previous {
        sendNotification(
            "🟢 **Minecraft online**\n" +
                "El servidor de Minecraft está disponible.",
        )
    }

    // Online -> Offline
    if !online && previous {
        sendNotification(
            "🔴 **Minecraft offline**\n" +
                "El servidor de Minecraft ha dejado de responder.",
        )
    }
}
func monitorMinecraft() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		state.RLock()
		online := state.MinecraftOnline
		state.RUnlock()

		if !online {
			continue
		}

		if time.Since(lastMinecraftHeartbeat) > 90*time.Second {
			state.Lock()

			state.MinecraftOnline = false
			state.Players = make(map[string]bool)

			state.Unlock()

			sendNotification(
				"🔴 **Minecraft offline**\n" +
					"El servidor de Minecraft ha dejado de responder.",
			)
		}
	}
}
func handleMinecraftJoin(event MinecraftEvent) {
	state.Lock()

	state.MinecraftOnline = true
	state.Players[event.Player] = true

	state.Unlock()

	sendNotification(
		"🟢 **Jugador conectado**\n" +
			"`" + event.Player + "` ha entrado al servidor.",
	)
}
func handleMinecraftQuit(event MinecraftEvent) {
	state.Lock()

	delete(state.Players, event.Player)

	state.Unlock()

	sendNotification(
		"🔴 **Jugador desconectado**\n" +
			"`" + event.Player + "` ha salido del servidor.",
	)
}
func handleMinecraftDeath(event MinecraftEvent) {
	message := event.Message

	if message == "" {
		message = event.Player + " ha muerto."
	}

	sendNotification(
		"💀 **Muerte**\n" +
			"`" + message + "`",
	)
}
func handleMinecraftChat(event MinecraftEvent) {
	sendNotification(
		"💬 **Minecraft**\n" +
			"**" + event.Player + ":** " +
			event.Message,
	)
}
