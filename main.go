package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gocolly/colly"
	"golang.org/x/net/html"
)

func getElementById(id string, n *html.Node) (element *html.Node, nodeFound bool) {
	for _, a := range n.Attr {
		if a.Key == "id" && a.Val == id {
			return n, true
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if element, nodeFound = getElementById(id, c); nodeFound {
			return
		}
	}
	return
}

func getPlateFromText(s string) string {
	re := regexp.MustCompile(`[a-zA-Z]{3}\d{3}`)
	return re.FindString(s)
}

func getDocumentAsString(url string) string {
	response, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()

	dataInBytes, err := ioutil.ReadAll(response.Body)
	return string(dataInBytes)
}

type carPrice struct {
	Price string `json:"valued_dealer_price"`
}

func getCarList(url string) (html.Node, error) {

	docString := getDocumentAsString("https://www.blocket.se/goteborg/bilar?cg=1020&w=1&st=s&ps=2&pe=17&mys=2014&me=30&ca=15&is=1&l=0&md=th&cb=41")

	doc, _ := html.Parse(strings.NewReader(docString))
	carListNode, nodeFound := getElementById("item_list", doc)
	if !nodeFound {
		log.Fatal("Car node not found")
	}
	return *carListNode, nil
}

func getCarPrice(c *car) {

	httpClient := http.Client{
		Timeout: time.Second * 2, // Maximum of 2 secs
	}

	url := fmt.Sprintf(`https://www.bilpriser.se/api/?bpapi_action=get_values&model_id=%s&y=%s&distance=%s&value_decrement_start=%s&regnr=%s`, c.modelId, c.year, c.distance, c.regdate, c.plate)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Fatal(err)
	}

	req.Header.Set("User-Agent", "Test")

	res, getErr := httpClient.Do(req)
	if getErr != nil {
		log.Fatal(getErr)
	}

	body, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		log.Fatal(readErr)
	}

	cd := carPrice{}
	if jsonErr := json.Unmarshal(body, &cd); jsonErr != nil {
		log.Fatal(jsonErr)
		return
	}

	fmt.Println("Bilpriser price found: ", cd.Price)
	c.estPrice = cd.Price
}

type vehicle struct {
	Vehicle carData `json:"vehicle"`
}

type carData struct {
	ModelId  string `json:"model_id"`
	Distance int    `json:"estimated_distance"`
	Year     string `json:"model_year"`
	RegDate  string `json:"registration_date"`
}

func getCarInfo(c *car) {

	httpClient := http.Client{
		Timeout: time.Second * 2, // Maximum of 2 secs
	}

	req, err := http.NewRequest(http.MethodGet, "https://www.bilpriser.se/api/?bpapi_action=get_vehicle_registry_se&regnr="+c.plate, nil)
	if err != nil {
		log.Fatal(err)
		return
	}

	req.Header.Set("User-Agent", "Test")

	res, getErr := httpClient.Do(req)
	if getErr != nil {
		log.Fatal(getErr)
	}

	body, readErr := ioutil.ReadAll(res.Body)
	if readErr != nil {
		log.Fatal(readErr)
	}

	cd := vehicle{}
	if jsonErr := json.Unmarshal(body, &cd); jsonErr != nil {
		log.Fatal(jsonErr)
		return
	}
	c.modelId = cd.Vehicle.ModelId
	c.distance = strconv.Itoa(cd.Vehicle.Distance)
	c.year = cd.Vehicle.Year
	c.regdate = cd.Vehicle.RegDate
}

type car struct {
	estPrice  string
	link      string
	listPrice string
	diff      string
	plate     string
	modelId   string
	distance  string
	year      string
	regdate   string
}

func saveToCsv(cars *map[string]*car) {
	fmt.Println("Time to save...")
	file, err := os.Create("result.csv")
	if err != nil {
		fmt.Println("Cannot create file", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	headers := []string{
		"Plate",
		"Estimated",
		"Blocket",
		"Diff",
		"Link",
		"Model Id",
		"Distance",
		"Year",
		"Reg Date",
	}
	writer.Write(headers)

	for _, car := range *cars {

		if len(getFieldString(car, "plate")) == 0 {
			continue
		}
		s := make([]string, 0)

		s = append(s, getFieldString(car, "plate"))
		s = append(s, getFieldString(car, "estPrice"))
		s = append(s, getFieldString(car, "listPrice"))
		s = append(s, getFieldString(car, "diff"))
		s = append(s, getFieldString(car, "link"))
		s = append(s, getFieldString(car, "modelId"))
		s = append(s, getFieldString(car, "distance"))
		s = append(s, getFieldString(car, "year"))
		s = append(s, getFieldString(car, "regdate"))
		writer.Write(s)
	}

	fmt.Println("Done saving")
}

func getFieldString(c *car, field string) string {
	r := reflect.ValueOf(c)
	f := reflect.Indirect(r).FieldByName(field)
	return f.String()
}

func getFieldInteger(c *car, field string) int {
	r := reflect.ValueOf(c)
	f := reflect.Indirect(r).FieldByName(field)
	return int(f.Int())
}

func main() {

	// Track cars
	cars := make(map[string]*car)

	c := colly.NewCollector()

	activeIndex := "0"

	c.OnHTML("a.item-link", func(e *colly.HTMLElement) {
		link := e.Attr("href")
		// Print link
		fmt.Printf("Link found: %q -> %s\n", e.Text, link)
		activeIndex = strconv.Itoa(e.Index)
		cars[strconv.Itoa(e.Index)] = &car{link: link}
		// Visit link found on page
		c.Visit(e.Request.AbsoluteURL(link))
	})

	c.OnHTML("p.list_price", func(e *colly.HTMLElement) {
		reg, err := regexp.Compile("[^0-9]+")
		if err != nil {
			log.Fatal(err)
		}
		price := reg.ReplaceAllString(e.Text, "")
		if len(price) > 6 {
			price = price[0:6]
		}
		cars[strconv.Itoa(e.Index)].listPrice = price

		estAsInt, err := strconv.Atoi(cars[strconv.Itoa(e.Index)].estPrice)
		priceAsInt, err := strconv.Atoi(price)
		cars[strconv.Itoa(e.Index)].diff = strconv.Itoa(estAsInt - priceAsInt)

		fmt.Println("Blocket price found: ", price)
	})

	c.OnHTML("aside.body_aside", func(e *colly.HTMLElement) {
		plate := getPlateFromText(e.Text)
		if len(plate) > 0 {
			fmt.Println("Plate found: ", plate)
			cars[activeIndex].plate = plate
			getCarInfo(cars[activeIndex])
			getCarPrice(cars[activeIndex])
		}
	})

	// Before making a request print "Visiting ..."
	c.OnRequest(func(r *colly.Request) {
		fmt.Println("Visiting", r.URL.String())
	})

	c.OnError(func(r *colly.Response, e error) {
		log.Println("error:", e, r.Request.URL, string(r.Body))
	})

	// Start scraping on blocket.se
	c.Visit("https://www.blocket.se/hela_sverige?q=d4&cg=1020&w=3&st=s&ps=3&pe=19&mys=2014&mye=2015&ms=&me=26&cxpf=8&cxpt=&fu=&pl=&gb=&ca=15&is=1&l=0&md=th&sp=1&cb=41")

	saveToCsv(&cars)

}
