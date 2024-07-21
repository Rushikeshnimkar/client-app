package main

import (
	"bufio"
	"chicken/api"
	"chicken/callcontract"
	"chicken/utils"
	"chicken/wifi"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/fatih/color"
	"github.com/joho/godotenv"
)

var userWalletAddress common.Address

func main() {
	utils.ErebrusBanner()
	err := godotenv.Load()
	if err != nil {
		color.Red("Error loading .env file")
		return
	}

	// Ask for user's wallet address
	reader := bufio.NewReader(os.Stdin)
	color.Cyan("Please enter your wallet address: ")
	addressStr, _ := reader.ReadString('\n')
	addressStr = strings.TrimSpace(addressStr)
	if !common.IsHexAddress(addressStr) {
		color.Red("Invalid Ethereum address")
		return
	}
	userWalletAddress = common.HexToAddress(addressStr)

	client, err := ethclient.Dial(os.Getenv("ETHEREUM_NODE_URL"))
	if err != nil {
		color.Red("Failed to connect to the Ethereum client: %v", err)
		return
	}

	for {
		// Scan for nearby WiFi networks
		color.Cyan("Scanning for nearby WiFi networks...")
		nearbyNetworks, err := wifi.ScanNearbyWiFi()
		if err != nil {
			color.Red("Failed to scan for nearby networks: %v", err)
			return
		}

		// Fetch WiFi data from HTTP API with retry
		color.Cyan("Fetching WiFi data from API...")
		wifiDataList, err := fetchWiFiDataWithRetry()
		if err != nil {
			color.Red("Failed to get WiFi data from API after retry: %v", err)
			color.Yellow("Continuing with local data only...")
			wifiDataList = []wifi.WiFiData{} // Use an empty list if API fetch fails
		}

		// Create a map to store unique SSIDs with their latest information
		ssidInfoMap := make(map[string]struct {
			ID          uint
			Password    string
			PricePerMin string
			ChainName   string
		})

		// Update SSID information from API data
		for _, wifiData := range wifiDataList {
			for _, status := range wifiData.Status {
				if len(status.HostSSID) > 0 {
					ssidLower := strings.ToLower(status.HostSSID)
					ssidInfoMap[ssidLower] = struct {
						ID          uint
						Password    string
						PricePerMin string
						ChainName   string
					}{
						ID:          wifiData.ID,
						Password:    wifiData.Password,
						PricePerMin: wifiData.PricePerMin,
						ChainName:   wifiData.ChainName,
					}
				}
			}
		}

		// Find common SSIDs between nearby networks and API data
		color.Green("Erebrus Registered DWiFi Networks in Your Vicinity:")
		var readyNetworks []string
		for i, ssid := range nearbyNetworks {
			ssidLower := strings.ToLower(ssid)
			if info, found := ssidInfoMap[ssidLower]; found {
				color.Yellow("%d. SSID: %s, Price per min: %s, Chain name: %s",
					i+1, ssid, info.PricePerMin, info.ChainName)
				readyNetworks = append(readyNetworks, ssid)
			}
		}

		if len(readyNetworks) == 0 {
			color.Yellow("No Erebrus DWiFi networks detected in your current location.")
			continue
		}

		// User selection
		fmt.Println(color.HiMagentaString("______________________________________________________________________________________________________"))
		color.Cyan("Enter the number of the WiFi network you want to connect to, or '0' to exit: ")
		choiceStr, _ := reader.ReadString('\n')
		choiceStr = strings.TrimSpace(choiceStr)

		if choiceStr == "0" {
			color.Green("Exiting program. Goodbye!")
			return
		}

		choice, err := strconv.Atoi(choiceStr)
		if err != nil || choice < 1 || choice > len(readyNetworks) {
			color.Red("Invalid choice")
			continue
		}

		selectedSSID := readyNetworks[choice-1]
		selectedSSIDLower := strings.ToLower(selectedSSID)
		info := ssidInfoMap[selectedSSIDLower]

		color.Cyan("Connecting to %s...", selectedSSID)
		err = wifi.ConnectToWiFi(selectedSSID, info.Password)
		if err != nil {
			color.Red("Error connecting to WiFi: %v", err)
			continue
		}

		color.Green("Successfully connected to %s", selectedSSID)
		startTime := time.Now()

		disconnectChan := make(chan bool)
		go func() {
			for {
				reader := bufio.NewReader(os.Stdin)
				input, _ := reader.ReadString('\n')
				input = strings.TrimSpace(input)

				if input == "d" {
					disconnectChan <- true
					return
				}
			}
		}()

		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

	connectionLoop:
		for {
			select {
			case <-ticker.C:
				connectedTime := time.Since(startTime).Round(time.Second)
				color.Cyan("Connected to %s for %s", selectedSSID, connectedTime)
				color.Yellow("Press 'd' to disconnect and pay")
			case <-disconnectChan:
				connectedTime := time.Since(startTime).Round(time.Second)
				totalMinutes := float64(connectedTime.Minutes())
				pricePerMin, err := strconv.ParseFloat(info.PricePerMin, 64)
				if err != nil {
					color.Red("Error parsing price per minute: %v", err)
					pricePerMin = 0
				}
				totalCost := totalMinutes * pricePerMin

				color.Green("Total connected time: %s", connectedTime)
				color.Green("Total cost: %.2f AGNG (Price per min: %s AGNG)", totalCost, info.PricePerMin)
				disconnectAndPay(client)
				break connectionLoop
			}
		}

		color.Green("Returning to main menu...")
		time.Sleep(3 * time.Second)
	}
}

func fetchWiFiDataWithRetry() ([]wifi.WiFiData, error) {
	retryCount := 2
	var wifiDataList []wifi.WiFiData
	var err error

	for i := 0; i < retryCount; i++ {
		wifiDataList, err = api.GetWiFiDataFromHTTP()
		if err == nil {
			return wifiDataList, nil
		}

		if strings.Contains(err.Error(), "no such host") {
			color.Yellow("Failed to fetch WiFi data. Retrying in 5 seconds...")
			time.Sleep(5 * time.Second)
		} else {
			return nil, err
		}
	}

	return nil, err
}
func disconnectAndPay(client *ethclient.Client) {
	color.Yellow("Initiating disconnection and payment process...")

	err := callcontract.MintAndPay(client, userWalletAddress)
	if err != nil {
		color.Red("Error in payment process: %v", err)
		return
	}

	// Disconnect from WiFi
	err = wifi.DisconnectFromWiFi()
	if err != nil {
		color.Red("Error disconnecting from WiFi: %v", err)
	} else {
		color.Green("Successfully disconnected from WiFi")
	}
}
