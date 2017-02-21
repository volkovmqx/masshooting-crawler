package main

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/NaySoftware/go-fcm"
	"golang.org/x/net/html"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

const (
	serverKey = "SERVER_KEY"
)

// Incident : to store all the shoootings
type Incident struct {
	ID      int
	Date    string
	City    string
	Address string
	Lat     float64
	Lng     float64
	Killed  int
	Injured int
	News    string
}

// Cordinnates : base type for a cordinnate
type Cordinnates struct {
	Number int
	Lat    string
	Lng    string
}

// Client : client informations
type Client struct {
	Imei  string
	Lat   float64
	Lng   float64
	Token string
	Range float64
}

func getHref(t html.Token) (ok bool, href string) {
	// Iterate over all of the Token's attributes until we find an "href"
	for _, a := range t.Attr {
		if a.Key == "href" {
			href = a.Val
			ok = true
		}
	}

	// "bare" return will return the variables (ok, href) as defined in
	// the function definition
	return
}

func crawldata(url string, ch chan Cordinnates, chFinished chan bool, n int) {
	resp, err := http.Get(url)
	defer func() {
		// Notify that we're done after this function
		chFinished <- true
	}()
	if err != nil {
		fmt.Println("ERROR: Failed to crawl \"" + url + "\"")
		return
	}

	b := resp.Body
	defer b.Close() // close Body when the function returns

	z := html.NewTokenizer(b)
	for {
		tt := z.Next()

		switch {
		case tt == html.ErrorToken:
			// End of the document, we're done
			return
		case tt == html.TextToken:
			t := z.Token()
			if strings.HasPrefix(t.Data, "Geolo") {
				var cor Cordinnates
				cor.Number = n
				cor.Lat = strings.Split(strings.TrimPrefix(t.Data, "Geolocation: "), ", ")[0]
				cor.Lng = strings.Split(strings.TrimPrefix(t.Data, "Geolocation: "), ", ")[1]
				ch <- cor
			}
		}
	}
}

// haversin(θ) function
func hsin(theta float64) float64 {
	return math.Pow(math.Sin(theta/2), 2)
}

// Distance function returns the distance (in meters) between two points of
// http://en.wikipedia.org/wiki/Haversine_formula
func Distance(lat1, lon1, lat2, lon2 float64) float64 {
	// convert to radians
	// must cast radius as float to multiply later
	var la1, lo1, la2, lo2, r float64
	la1 = lat1 * math.Pi / 180
	lo1 = lon1 * math.Pi / 180
	la2 = lat2 * math.Pi / 180
	lo2 = lon2 * math.Pi / 180

	r = 6378100 // Earth radius in METERS

	// calculate
	h := hsin(la2-la1) + math.Cos(la1)*math.Cos(la2)*hsin(lo2-lo1)

	return 2 * r * math.Asin(math.Sqrt(h))
}

func isInRange(client Client, accident Incident) (isInRange bool, distance float64) {
	distance = Distance(client.Lat, client.Lng, accident.Lat, accident.Lng)
	if distance <= client.Range {
		isInRange = true
	} else {
		isInRange = false
	}
	return
}

func main() {
	foundData := make([]Incident, 0, 25)
	chData := make(chan Cordinnates)
	chFinished := make(chan bool)
	links := 0

	url := "http://www.gunviolencearchive.org/reports/mass-shooting"
	resp, err := http.Get(url)

	if err != nil {
		fmt.Println("ERROR: Failed to crawl \"" + url + "\"")
		return
	}

	b := resp.Body
	defer b.Close()

	z := html.NewTokenizer(b)
	id := 0
FOR:
	for {
		tt := z.Next()

		switch {
		case tt == html.ErrorToken:
			// End of the document, we're done Terminate the for
			break FOR
		case tt == html.StartTagToken:
			t := z.Token()
			if t.Data == "a" {

				id = 0
				ok, href := getHref(t)
				if !ok {
					continue
				}

				if strings.Index(href, "/incident") == 0 {
					// get news
					for i := 0; i < 6; i++ {
						tt = z.Next()
					}
					t := z.Token()
					ok, newshref := getHref(t)
					if ok {
						foundData[links].News = newshref
					}

					foundData[links].ID, _ = strconv.Atoi(strings.TrimPrefix(href, "/incident/"))
					links++
					go crawldata("http://www.gunviolencearchive.org"+href, chData, chFinished, links-1)
				}
			}
			if t.Data == "td" {
				tt = z.Next()
				t := z.Token()
				switch {
				case tt == html.TextToken:
					id++
					switch {
					case id == 1:
						// resize the slice
						foundData = foundData[0 : links+1]
						foundData[links].Date = t.Data
						// Date
					case id == 3:
						foundData[links].City = t.Data
						// City
					case id == 4:
						foundData[links].Address = t.Data
						// Adress
					case id == 5:
						foundData[links].Killed, _ = strconv.Atoi(t.Data)
						// Killed
					case id == 6:
						foundData[links].Injured, _ = strconv.Atoi(t.Data)
						// Injured
					}

				}
			}
		}
	}

	// Subscribe to channels
	for c := 0; c < links; {
		select {
		case data := <-chData:
			foundData[data.Number].Lat, err = strconv.ParseFloat(data.Lat, 64)
			foundData[data.Number].Lng, err = strconv.ParseFloat(data.Lng, 64)
		case <-chFinished:
			c++
		}
	}
	// database
	session, err := mgo.Dial("127.0.0.1")
	if err != nil {
		panic(err)
	}
	defer session.Close()

	// Optional. Switch the session to a monotonic behavior.
	//session.SetMode(mgo.Monotonic, true)

	db := session.DB("shooting")

	var clients []Client

	err = db.C("clients").Find(nil).All(&clients)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%v", clients)

	for _, data := range foundData {
		changeinfo, err := db.C("accidents").Upsert(bson.M{"id": data.ID}, &data)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%+v\n", changeinfo)
		// there is new data, we send notifications to users
		if changeinfo.Matched == 0 {
			for _, client := range clients {
				ok, dist := isInRange(client, data)
				// the mobile is in range of the accident
				if ok {
					notification := map[string]string{
						"data": "In range",
					}

					ids := []string{
						client.Token,
					}
					c := fcm.NewFcmClient(serverKey)
					payload := &fcm.NotificationPayload{
						"New Shooting nearby !",
						"There is a Mass shooting in approx. " + strconv.FormatFloat(dist, 'f', 2, 64) + " meters.Stay Safe!",
						"notification",
						"default", "", "", "", "", "", "", "", "",
					}
					c.SetNotificationPayload(payload)
					c.NewFcmRegIdsMsg(ids, notification)

					status, err := c.Send()

					if err == nil {
						status.PrintResults()
					} else {
						fmt.Println(err)
					}

				}

			}
		}
		fmt.Printf("%+v\n", data)
	}
	defer close(chData)
	// var input string
	// fmt.Scanln(&input)
}
