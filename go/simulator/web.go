/*
 * © 2025 Sharon Aicler (saichler@gmail.com)
 *
 * Layer 8 Ecosystem is licensed under the Apache License, Version 2.0.
 * You may obtain a copy of the License at:
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/pprof"
	"time"

	"github.com/gorilla/mux"
)

// Web handlers for HTTP API endpoints

func createDevicesHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateDevicesRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		sendErrorResponse(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.DeviceCount <= 0 {
		sendErrorResponse(w, "Device count must be greater than 0", http.StatusBadRequest)
		return
	}

	snmpPort := req.SNMPPort
	if snmpPort == 0 {
		snmpPort = DEFAULT_SNMP_PORT
	}
	if snmpPort < 1 || snmpPort > 65535 {
		sendErrorResponse(w, "snmp_port must be between 1 and 65535", http.StatusBadRequest)
		return
	}

	// Use CreateDevicesWithOptions if pre-allocation parameters are specified
	if req.PreAllocate || req.MaxWorkers > 0 {
		// If PreAllocate is not explicitly set but MaxWorkers is provided, enable pre-allocation
		preAllocate := req.PreAllocate || req.MaxWorkers > 0
		err = manager.CreateDevicesWithOptions(req.StartIP, req.DeviceCount, req.Netmask, req.ResourceFile, req.SNMPv3, preAllocate, req.MaxWorkers, req.RoundRobin, req.Category, snmpPort)
	} else {
		// Use default behavior (auto pre-allocates for 10+ devices)
		err = manager.CreateDevices(req.StartIP, req.DeviceCount, req.Netmask, req.ResourceFile, req.SNMPv3, req.RoundRobin, req.Category, snmpPort)
	}
	if err != nil {
		sendErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sendSuccessResponse(w, fmt.Sprintf("Created %d devices starting from %s", req.DeviceCount, req.StartIP))
}

func listDevicesHandler(w http.ResponseWriter, r *http.Request) {
	devices := manager.ListDevices()
	sendDataResponse(w, devices)
}

func listResourcesHandler(w http.ResponseWriter, r *http.Request) {
	resources := manager.ListAvailableResources()
	sendDataResponse(w, resources)
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	status := manager.GetStatus()
	sendDataResponse(w, status)
}

func systemStatsHandler(w http.ResponseWriter, r *http.Request) {
	stats := GetSystemStats()
	sendDataResponse(w, stats)
}

func deleteDeviceHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	deviceID := vars["id"]

	err := manager.DeleteDevice(deviceID)
	if err != nil {
		sendErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	sendSuccessResponse(w, fmt.Sprintf("Device %s deleted", deviceID))
}

func deleteAllDevicesHandler(w http.ResponseWriter, r *http.Request) {
	err := manager.DeleteAllDevices()
	if err != nil {
		sendErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sendSuccessResponse(w, "All devices deleted")
}

func exportDevicesCSVHandler(w http.ResponseWriter, r *http.Request) {
	devices := manager.ListDevices()

	// Set headers for CSV download
	filename := fmt.Sprintf("devices_%s.csv", time.Now().Format("2006-01-02_15-04-05"))
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	// Create CSV writer
	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write CSV headers
	headers := []string{"Device ID", "IP Address", "Interface", "SNMP Port", "SSH Port", "Status"}
	if err := writer.Write(headers); err != nil {
		http.Error(w, "Failed to write CSV headers", http.StatusInternalServerError)
		return
	}

	// Write device data
	for _, device := range devices {
		status := "Stopped"
		if device.Running {
			status = "Running"
		}

		interfaceName := device.Interface
		if interfaceName == "" {
			interfaceName = "N/A"
		}

		record := []string{
			device.ID,
			device.IP,
			interfaceName,
			fmt.Sprintf("%d", device.SNMPPort),
			fmt.Sprintf("%d", device.SSHPort),
			status,
		}

		if err := writer.Write(record); err != nil {
			http.Error(w, "Failed to write CSV record", http.StatusInternalServerError)
			return
		}
	}
}

func generateRouteScriptHandler(w http.ResponseWriter, r *http.Request) {
	devices := manager.ListDevices()

	// Set headers for script download
	filename := fmt.Sprintf("add_simulator_routes_%s.sh", time.Now().Format("2006-01-02_15-04-05"))
	w.Header().Set("Content-Type", "application/x-sh")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	// Generate bash script content
	script := generateRouteScript(devices)
	w.Write([]byte(script))
}

func pprofMemoryHandler(w http.ResponseWriter, r *http.Request) {
	filename := fmt.Sprintf("opensim_heap_%s.pprof", time.Now().Format("2006-01-02_15-04-05"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	if err := pprof.WriteHeapProfile(w); err != nil {
		http.Error(w, "Failed to write heap profile", http.StatusInternalServerError)
	}
}

func cpuProfileHandler(w http.ResponseWriter, r *http.Request) {
	filename := fmt.Sprintf("opensim_cpu_%s.pprof", time.Now().Format("2006-01-02_15-04-05"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	if err := pprof.StartCPUProfile(w); err != nil {
		http.Error(w, "Failed to start CPU profile: "+err.Error(), http.StatusInternalServerError)
		return
	}
	time.Sleep(5 * time.Second)
	pprof.StopCPUProfile()
}

// Helper functions for API responses
func sendSuccessResponse(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Message: message,
	})
}

func sendDataResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Message: "Success",
		Data:    data,
	})
}

func sendErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(APIResponse{
		Success: false,
		Message: message,
	})
}

// Web UI handler - serves the index.html from web directory
func webUIHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "web/index.html")
}

// Setup REST API routes
func setupRoutes() *mux.Router {
	router := mux.NewRouter()

	// Web UI
	router.HandleFunc("/", webUIHandler).Methods("GET")
	router.HandleFunc("/ui", webUIHandler).Methods("GET")

	// Static web assets (CSS, JS)
	router.PathPrefix("/web/").Handler(http.StripPrefix("/web/", http.FileServer(http.Dir("web"))))

	// API routes
	api := router.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/devices", createDevicesHandler).Methods("POST")
	api.HandleFunc("/devices", listDevicesHandler).Methods("GET")
	api.HandleFunc("/devices/export", exportDevicesCSVHandler).Methods("GET")
	api.HandleFunc("/devices/routes", generateRouteScriptHandler).Methods("GET")
	api.HandleFunc("/devices/{id}", deleteDeviceHandler).Methods("DELETE")
	api.HandleFunc("/devices", deleteAllDevicesHandler).Methods("DELETE")
	api.HandleFunc("/resources", listResourcesHandler).Methods("GET")
	api.HandleFunc("/status", statusHandler).Methods("GET")
	api.HandleFunc("/system-stats", systemStatsHandler).Methods("GET")
	api.HandleFunc("/debug/pprof-memory", pprofMemoryHandler).Methods("GET")
	api.HandleFunc("/debug/cpu-profile", cpuProfileHandler).Methods("GET")

	// Static file for logo
	router.HandleFunc("/logo.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		http.ServeFile(w, r, "web/logo.png")
	}).Methods("GET", "HEAD")

	// Health check
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}).Methods("GET")

	return router
}
