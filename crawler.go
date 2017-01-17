package main
import(
  "fmt"
  "net/http"
  "strings"
  "golang.org/x/net/html"
  "strconv"
  "log"
  "gopkg.in/mgo.v2"
  "gopkg.in/mgo.v2/bson"
)
type Incident struct {
  Id int
  Date string
  City	 string
  Address string
  Cordinnates string
  Killed int
  Injured int
  News string
}
type Cordinnates struct {
  Number int
  LatLng string
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
			if strings.HasPrefix(t.Data,"Geolo") {
        var cor Cordinnates
        cor.Number = n
        cor.LatLng = strings.TrimPrefix(t.Data,"Geolocation: ")
        ch <- cor
			}
		}
	}
}

func main() {
  foundData := make([]Incident, 0,25)
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
          for i:=0;i<6;i++ {
              tt = z.Next()
          }
          t := z.Token()
          ok, newshref := getHref(t)
          if ok {
            foundData[links].News = newshref
          }

          foundData[links].Id,_ = strconv.Atoi(strings.TrimPrefix(href,"/incident/"))
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
                foundData = foundData[0:links+1]
                foundData[links].Date = t.Data
                // Date
              case id == 3:
                foundData[links].City = t.Data
                // City
              case id == 4:
                foundData[links].Address = t.Data
                // Adress
              case id == 5:
                foundData[links].Killed,_ = strconv.Atoi(t.Data)
                // Killed
              case id == 6:
                foundData[links].Injured,_ = strconv.Atoi(t.Data)
                // Injured
            }

      }
    }
  }
  }

  // Subscribe to channels
  for c := 0; c < links; {
    select {
    case data := <- chData:
      foundData[data.Number].Cordinnates = data.LatLng
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
       session.SetMode(mgo.Monotonic, true)

       c := session.DB("shooting").C("accidents")

  for _ , data := range foundData {
    _,err = c.Upsert(bson.M{"id": data.Id},&data)
    if err != nil {
            log.Fatal(err)
    }
    fmt.Printf("%+v\n", data)
  }
  defer close(chData)
  // var input string
  // fmt.Scanln(&input)
}
