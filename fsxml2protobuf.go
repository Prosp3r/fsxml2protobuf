package main

import (
	"bufio"
	"bytes"
	"code.google.com/p/goprotobuf/proto"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"github.com/DallanQ/fsxml2protobuf/fs_data"
	"github.com/codegangsta/cli"
	"github.com/willf/bloom"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

var stdPlaces map[string]string
var sourceRefs map[string][]string
var personIdsBloom *bloom.BloomFilter
var personIdsMutex = &sync.Mutex{}

// Data is the global BFF record structure
type Data struct {
	Records []Record `xml:"record"`
}
// Record contains people and relationships
type Record struct {
	Person        Person         `xml:"person"`
	Relationships []Relationship `xml:"relationship"`
}
// Person contains information about a person
type Person struct {
	ID     string `xml:"id,attr"`
	Gender Gender `xml:"gender"`
	Names  []Name `xml:"name"`
	Facts  []Fact `xml:"fact"`
}
// Relationship contains information about a relationship
type Relationship struct {
	Type    string         `xml:"type,attr"` // http://gedcomx.org/ParentChild or http://gedcomx.org/Couple
	Person1 PersonResource `xml:"person1"`   // https://familysearch.org/ark:/61903/4:1:K8PV-6M7 or #218J-DF3
	Person2 PersonResource `xml:"person2"`
	Facts   []Fact         `xml:"fact"`
}
// PersonResource contains a resource id
type PersonResource struct {
	Resource string `xml:"resource,attr"`
}
// Gender contains the gender fact
type Gender struct {
	Type        string      `xml:"type,attr"` // http://gedcomx.org/Male or Female or Unknown
	Attribution Attribution `xml:"attribution"`
}
// Name we don't capture names; only their attribution
type Name struct {
	Attribution Attribution `xml:"attribution"`
}
// Fact contains all other facts
type Fact struct {
	Type        string      `xml:"type,attr"`
	Attribution Attribution `xml:"attribution"`
	Date        Date        `xml:"date"`
	Place       Place       `xml:"place"`
}
// Date we only capture the user-entered text
type Date struct {
	Original string `xml:"original"`
}
// Place we only capture the user-entered text
type Place struct {
	Original string `xml:"original"`
}
// Attribution contains the contributor
type Attribution struct {
	Contributor Contributor `xml:"contributor"`
}
// Contributor contains information about the user
type Contributor struct {
	ResourceID string `xml:"resourceId,attr"`
}

func getGender(person *Person) fs_data.FSGender {
	var gender fs_data.FSGender
	if person.Gender.Type == "http://gedcomx.org/Male" {
		gender = fs_data.FSGender_MALE
	} else if person.Gender.Type == "http://gedcomx.org/Female" {
		gender = fs_data.FSGender_FEMALE
	} else {
		gender = fs_data.FSGender_UNKNOWN
	}
	return gender
}

func getContributors(person *Person, relationships []Relationship) []string {
	contributors := make(map[string]bool)
	contributors[person.Gender.Attribution.Contributor.ResourceID] = true
	for _, name := range person.Names {
		contributors[name.Attribution.Contributor.ResourceID] = true
	}
	for _, fact := range person.Facts {
		contributors[fact.Attribution.Contributor.ResourceID] = true
	}
	for _, relationship := range relationships {
		for _, fact := range relationship.Facts {
			contributors[fact.Attribution.Contributor.ResourceID] = true
		}
	}

	var result []string
	for contributor := range contributors {
		if contributor != "" {
			result = append(result, contributor)
		}
	}
	return result
}

func getSources(person *Person) []*fs_data.FSSource {
	var sources []*fs_data.FSSource
	for _, ref := range sourceRefs[person.ID] {
		sources = append(sources, &fs_data.FSSource{SourceId: &ref})
	}
	return sources
}

func getGedcomXLabel(url string) string {
	return url[strings.LastIndex(url, "/")+1:]
}

var yearRegex = regexp.MustCompile("\\b\\d{4}\\b")

func getYear(date string) int32 {
	s := yearRegex.FindString(date)
	if s != "" {
		year, _ := strconv.ParseInt(s, 10, 32)
		return int32(year)
	}
	return 0
}

func getStdPlace(place string) string {
	place = strings.Replace(place, "\t", " ", -1)
	stdPlace := stdPlaces[place]
	if stdPlace != "" {
		return stdPlace
	} else {
		return place
	}
}

func getFact(fact Fact) *fs_data.FSFact {
	t := getGedcomXLabel(fact.Type)
	fsFact := &fs_data.FSFact{
		Type: &t,
	}
	year := getYear(fact.Date.Original)
	if year != 0 {
		fsFact.Year = &year
	}
	place := getStdPlace(fact.Place.Original)
	if place != "" {
		fsFact.Place = &place
	}
	return fsFact
}

func getFacts(person *Person, relationships []Relationship) []*fs_data.FSFact {
	var fsFacts []*fs_data.FSFact
	for _, fact := range person.Facts {
		fsFact := getFact(fact)
		fsFacts = append(fsFacts, fsFact)
	}
	for _, relationship := range relationships {
		for _, fact := range relationship.Facts {
			fsFact := getFact(fact)
			fsFacts = append(fsFacts, fsFact)
		}
	}
	return fsFacts
}

func getArkPid(ark string) string {
	return ark[strings.LastIndex(ark, ":")+1:]
}

func getRelationships(relationships []Relationship) ([]string, []string, []string) {
	var parents []string
	var children []string
	var spouses []string
	for _, relationship := range relationships {
		if relationship.Type == "http://gedcomx.org/ParentChild" {
			if strings.HasPrefix(relationship.Person1.Resource, "#") {
				relID := getArkPid(relationship.Person2.Resource)
				if relID != "" {
					children = append(children, relID)
				}
			} else {
				relID := getArkPid(relationship.Person1.Resource)
				if relID != "" {
					parents = append(parents, relID)
				}
			}
		} else { // Couple
			var relID string
			if strings.HasPrefix(relationship.Person1.Resource, "#") {
				relID = getArkPid(relationship.Person2.Resource)
			} else {
				relID = getArkPid(relationship.Person1.Resource)
			}
			if relID != "" {
				spouses = append(spouses, relID)
			}
		}
	}
	return parents, children, spouses
}

func readStdPlaces(file *os.File) map[string]string {
	stdPlaces := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Split(line, "\t")
		stdPlaces[fields[0]] = fields[1]
	}
	return stdPlaces
}

func readSourceRefs(file *os.File) map[string][]string {
	sourceRefs := make(map[string][]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.SplitN(line, ",", 2)
		sourceRefs[fields[0]] = append(sourceRefs[fields[0]], fields[1])
	}
	return sourceRefs
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func processFile(filename string, gzipOutput bool) int {
	inOut := strings.SplitN(filename, "\t", 2)
	inFilename := inOut[0]
	outFilename := inOut[1]
	var file io.ReadCloser
	var err error

	file, err = os.Open(inFilename)
	if err != nil {
		log.Printf("Error opening %s %v", inFilename, err)
		return 0
	}
	defer file.Close()

	if inFilename[len(inFilename)-3:] == ".gz" {
		file, err = gzip.NewReader(file)
		if err != nil {
			log.Printf("Error reading %s %v", inFilename, err)
			return 0
		}
		defer file.Close()
	}

	var data Data
	err = xml.NewDecoder(file).Decode(&data)
	if err != nil {
		log.Printf("Error decoding %s %v", inFilename, err)
		return 0
	}

	fsPersons := new(fs_data.FamilySearchPersons)
	fsPersons.Persons = make([]*fs_data.FamilySearchPerson, 0)

	cnt := 0
	for i := len(data.Records) - 1; i >= 0; i-- {
		person := data.Records[i].Person
		relationships := data.Records[i].Relationships

		// process each person only once
		// better go style might put this in a separate goroutine with channels to communicate
		personIdsMutex.Lock()
		isSeen := personIdsBloom.TestAndAdd([]byte(person.ID))
		personIdsMutex.Unlock()

		if !isSeen {
			gender := getGender(&person)
			parents, children, spouses := getRelationships(relationships)
			fsPerson := &fs_data.FamilySearchPerson{
				Id:           &person.ID,
				Gender:       &gender,
				Contributors: getContributors(&person, relationships),
				Sources:      getSources(&person),
				Facts:        getFacts(&person, relationships),
				Parents:      parents,
				Children:     children,
				Spouses:      spouses,
			}
			fsPersons.Persons = append(fsPersons.Persons, fsPerson)
			cnt++
		}
	}
	b, err := proto.Marshal(fsPersons)
	if err != nil {
		log.Printf("Error marshaling %s %v", inFilename, err)
		return 0
	}

	if gzipOutput {
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		w.Write(b)
		w.Close()
		b = buf.Bytes()
	}

	err = ioutil.WriteFile(outFilename, b, 0644)
	if err != nil {
		log.Printf("Error writing %s %v", outFilename, err)
		return 0
	}

	return cnt
}

func processFiles(fileNames chan string, results chan int, gzipOutput bool) {
	for fileName := range fileNames {
		results <- processFile(fileName, gzipOutput)
	}
}

func run(stdPlacesFilename string, sourceRefsFilename string, inFilename string, outFilename string, numWorkers int, gzipOutput bool) {
	numCPU := runtime.NumCPU()
	fmt.Printf("Number of CPUs=%d\n", numCPU)
	runtime.GOMAXPROCS(int(math.Min(float64(numCPU), float64(numWorkers))))

	numFiles := 0
	fileNames := make(chan string, 100000)
	fileInfo, err := os.Stat(inFilename)
	check(err)
	if fileInfo.IsDir() {
		personIdsBloom = bloom.New(80000000000, 70) // assume reading 800M people, p=1*E-21
		fileInfos, err := ioutil.ReadDir(inFilename)
		check(err)
		// process files (roughly) backwards to increase likelihood of processing latest version of each person
		for i := len(fileInfos) - 1; i >= 0; i-- {
			fileInfo = fileInfos[i]
			start := 0
			if strings.HasPrefix(fileInfo.Name(), "gedcomxb.") {
				start = len("gedcomxb.")
			}
			end := strings.Index(fileInfo.Name(), ".xml")
			suffix := ""
			if gzipOutput {
				suffix = ".gz"
			}
			fileNames <- inFilename + "/" + fileInfo.Name() + "\t" +
				outFilename + "/" + fileInfo.Name()[start:end] + ".protobuf" + suffix
			numFiles++
		}
	} else {
		personIdsBloom = bloom.New(3000000, 70) // assume reading 30K people, p=1*E-21
		fileNames <- inFilename + "\t" + outFilename
		numFiles++
	}
	close(fileNames)

	fmt.Println("Reading places")
	stdPlacesFile, err := os.Open(stdPlacesFilename)
	check(err)
	defer stdPlacesFile.Close()
	stdPlaces = readStdPlaces(stdPlacesFile)

	fmt.Println("Reading sources")
	sourceRefsFile, err := os.Open(sourceRefsFilename)
	check(err)
	defer sourceRefsFile.Close()
	sourceRefs = readSourceRefs(sourceRefsFile)

	fmt.Print("Processing files")
	results := make(chan int)

	for i := 0; i < numWorkers; i++ {
		go processFiles(fileNames, results, gzipOutput)
	}

	recordsProcessed := 0
	filesProcessed := 0
	for i := 0; i < numFiles; i++ {
		recordsProcessed += <-results
		filesProcessed++
		if filesProcessed%100 == 0 {
			fmt.Print(".")
		}
	}
	fmt.Printf("\nTotal files=%d records=%d\n", filesProcessed, recordsProcessed)
}

func main() {
	app := cli.NewApp()
	app.Name = "fsxml2protobuf"
	app.Usage = "Convert FamilySearch BFF xml files to protobuf format"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "places, p",
			Usage: "standardized places filename",
		},
		cli.StringFlag{
			Name:  "sourcerefs, s",
			Usage: "source references filename",
		},
		cli.StringFlag{
			Name:  "in, i",
			Usage: "input filename or directory",
		},
		cli.StringFlag{
			Name:  "out, o",
			Usage: "output filename or directory",
		},
		cli.IntFlag{
			Name:  "workers, w",
			Usage: "number of workers",
			Value: 16,
		},
		cli.BoolFlag{
			Name:  "gzip, z",
			Usage: "gzip output",
		},
	}
	app.Action = func(c *cli.Context) {
		run(c.String("places"), c.String("sourcerefs"), c.String("in"), c.String("out"), c.Int("workers"), c.Bool("gzip"))
	}
	app.Run(os.Args)
}
