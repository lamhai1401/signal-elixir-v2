package main

import (
	"encoding/json"
	"fmt"

	"github.com/lamhai1401/request/request"
	"github.com/lamhai1401/signal-elixir-v2/conn"
)

func main() {
	initElixir()

	select {}
}

func initElixir() error {
	domain := "https://signal-controller-staging-v2.quickom.com"
	apiURL := fmt.Sprintf("%s%s", domain, "/api")
	signalURL := fmt.Sprintf("%s%s", domain, "/socket/member/v1")

	alias := "yu4k5"

	// get auth
	chann, err := getAuth(apiURL, alias)
	if err != nil {
		return err
	}

	// wait for authen is resp
	authen := <-chann

	newConn := conn.NewConnection("localSignal", authen.MemberID, authen.Token, signalURL, nil)
	err = newConn.Start(&conn.Subscriber{
		Topic:        authen.MemberChannel,
		NeedSendping: true,
	})
	if err != nil {
		return err
	}

	return nil
}

// AuthSignal get from api for auth signal
type AuthSignal struct {
	Alias         string `json:"qr_code_alias"`
	RoomChannel   string `json:"room_channel"`
	RoomID        string `json:"room_id"`
	MemberChannel string `json:"member_channel"`
	MemberID      string `json:"member_id"`
	Role          string `json:"role"`
	Timestamp     int    `json:"timestamp"` // checking late message
	Token         string `json:"token"`
	Domain        string `json:"domain"`
}

func getAuth(apiURL string, alias string) (chan *AuthSignal, error) {
	chann := make(chan *AuthSignal, 1)
	api := request.NewAPI(3)

	// loop here
	go func() {
		subURL := "/member/v1/auth"
		url := fmt.Sprintf("%v%v", apiURL, subURL)

		header := map[string]string{
			"content-type": "application/json",
		}

		var result []byte
		var err error

		for {
			resp := api.POST(&url, header, map[string]interface{}{
				"alias": alias,
			})

			result, err = api.ReadResponse(<-resp)
			if err != nil {
				fmt.Println(err.Error())
				result = nil
				continue
			}

			// if result.StatusCode != 200 {
			// 	fmt.Printf("Has status code != 200, result err: %v \n", result)
			// 	result = nil
			// 	continue
			// }

			break
		}

		aut := &AuthSignal{}
		err = json.Unmarshal(result, &aut)
		if err != nil {
			fmt.Println(err.Error())
		} else {
			chann <- aut
		}
	}()

	return chann, nil
}
