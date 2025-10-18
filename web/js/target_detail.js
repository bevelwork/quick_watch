// Target Detail Page JavaScript
// Data is passed via data attributes on the body tag

// Get initialization data from HTML data attributes
const chartDataElement = document.body;
const chartData = JSON.parse(chartDataElement.dataset.chartData || '[]');
const checkStrategy = chartDataElement.dataset.checkStrategy || 'http';
const isPageComparison = checkStrategy === 'page-comparison';

// Format labels for display
const labels = chartData.map(d => {
    const date = new Date(d.timestamp);
    return date.toLocaleTimeString('en-US', { 
        hour: '2-digit', 
        minute: '2-digit', 
        second: '2-digit',
        hour12: false 
    });
});

// Helper function to format seconds with up to 4 significant digits
function formatSeconds(ms) {
    const seconds = ms / 1000;
    if (seconds === 0) return '0';
    return seconds.toPrecision(4);
}

const ctx = document.getElementById('responseChart').getContext('2d');
const chart = new Chart(ctx, {
    type: 'line',
    data: {
        labels: labels,
        datasets: [{
            label: isPageComparison ? 'Visual Difference (%)' : 'Response Time (s)',
            data: chartData.map(d => {
                if (!d.success) return null;
                return isPageComparison ? d.visualDifference : d.responseTime / 1000;
            }),
            borderColor: '#3fb950',
            backgroundColor: 'rgba(63, 185, 80, 0.1)',
            borderWidth: 2,
            tension: 0.4,
            pointRadius: 2,
            pointHoverRadius: 5,
            spanGaps: false,
            fill: true,
            pointBackgroundColor: chartData.map(d => d.success ? '#3fb950' : '#f85149'),
            pointBorderColor: chartData.map(d => d.success ? '#3fb950' : '#f85149'),
            pointHoverBackgroundColor: chartData.map(d => d.success ? '#3fb950' : '#f85149'),
            pointHoverBorderColor: chartData.map(d => d.success ? '#fff' : '#fff'),
            segment: {
                borderColor: ctx => {
                    const idx = ctx.p0DataIndex;
                    const p0Success = chartData[idx]?.success;
                    const p1Success = chartData[idx + 1]?.success;
                    if (!p0Success || !p1Success) {
                        return '#f85149';
                    }
                    return '#3fb950';
                }
            }
        }, {
            label: 'Failed Checks',
            data: chartData.map(d => !d.success ? (isPageComparison ? 100 : 0) : null),
            borderColor: '#f85149',
            backgroundColor: '#f85149',
            borderWidth: 0,
            pointRadius: 6,
            pointStyle: 'cross',
            pointHoverRadius: 8,
            showLine: false
        }]
    },
    options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: false,
        interaction: {
            intersect: false,
            mode: 'index'
        },
        plugins: {
            legend: {
                labels: {
                    color: '#c9d1d9',
                    font: {
                        size: 12
                    }
                }
            },
            tooltip: {
                backgroundColor: '#161b22',
                borderColor: '#30363d',
                borderWidth: 1,
                titleColor: '#f0f6fc',
                bodyColor: '#c9d1d9',
                padding: 12,
                displayColors: true,
                callbacks: {
                    title: function(context) {
                        const idx = context[0].dataIndex;
                        const data = window.chartData || chartData;
                        const date = new Date(data[idx].timestamp);
                        return date.toLocaleString();
                    },
                    label: function(context) {
                        const idx = context.dataIndex;
                        const data = window.chartData || chartData;
                        const entry = data[idx];
                        let label = context.dataset.label || '';
                        if (label) {
                            label += ': ';
                        }
                        if (entry.success) {
                            if (isPageComparison) {
                                label += entry.visualDifference.toFixed(2) + '%';
                            } else {
                                label += formatSeconds(entry.responseTime) + 's';
                            }
                        } else {
                            label += 'Failed';
                        }
                        return label;
                    }
                }
            }
        },
        scales: {
            x: {
                grid: {
                    color: '#30363d',
                    drawBorder: false
                },
                ticks: {
                    color: '#8b949e',
                    maxRotation: 45,
                    minRotation: 0,
                    maxTicksLimit: 10,
                    font: {
                        size: 11
                    }
                }
            },
            y: {
                beginAtZero: true,
                min: isPageComparison ? 0 : undefined,
                max: isPageComparison ? 100 : undefined,
                grid: {
                    color: '#30363d',
                    drawBorder: false
                },
                ticks: {
                    color: '#8b949e',
                    font: {
                        size: 11
                    },
                    callback: function(value) {
                        if (isPageComparison) {
                            return Math.round(value) + '%';
                        } else {
                            if (value === 0) return '0s';
                            const str = value.toPrecision(4);
                            return parseFloat(str) + 's';
                        }
                    }
                }
            }
        }
    }
});

// Track expanded entries
const expandedEntries = new Set();

// Track pause state
let isPaused = false;

// Toggle pause/unpause
function togglePause() {
    isPaused = !isPaused;
    const pauseButton = document.getElementById('pauseButton');
    
    if (isPaused) {
        pauseButton.textContent = '▶️ Resume';
        pauseButton.classList.add('paused');
    } else {
        pauseButton.textContent = '⏸️ Pause';
        pauseButton.classList.remove('paused');
        updateData();
    }
}

// Toggle target details section
function toggleDetails() {
    const content = document.getElementById('target-details-content');
    const toggleIcon = document.querySelector('.toggle-icon');
    const toggleText = document.querySelector('.toggle-text');
    
    if (content && toggleIcon && toggleText) {
        if (content.style.display === 'none') {
            content.style.display = 'block';
            toggleIcon.classList.add('expanded');
            toggleText.textContent = 'Hide Details';
        } else {
            content.style.display = 'none';
            toggleIcon.classList.remove('expanded');
            toggleText.textContent = 'Show Details';
        }
    }
}

// Toggle entry expansion
function toggleEntry(id) {
    const expandedDiv = document.getElementById('entry-' + id);
    const expandIcon = event.currentTarget.querySelector('.log-expand');
    
    if (expandedDiv && expandIcon) {
        if (expandedDiv.style.display === 'none') {
            expandedDiv.style.display = 'block';
            expandIcon.classList.add('expanded');
            expandedEntries.add(id);
        } else {
            expandedDiv.style.display = 'none';
            expandIcon.classList.remove('expanded');
            expandedEntries.delete(id);
        }
    }
}

// Auto-update data every 5 seconds
async function updateData() {
    if (isPaused) return;
    
    try {
        const response = await fetch(window.location.pathname.replace('/targets/', '/api/history/'));
        if (!response.ok) return;
        
        const data = await response.json();
        const history = data.history || [];
        
        // Update status badge
        const statusBadge = document.querySelector('.status-badge');
        if (statusBadge && data.target) {
            if (data.target.is_down) {
                if (data.target.acknowledged_at) {
                    statusBadge.className = 'status-badge acked';
                    statusBadge.textContent = '🔔 Acknowledged';
                } else {
                    statusBadge.className = 'status-badge down';
                    statusBadge.textContent = '❌ Down';
                }
            } else {
                statusBadge.className = 'status-badge healthy';
                statusBadge.textContent = '✅ Healthy';
            }
        }
        
        // Update acknowledge button
        const ackButtonContainer = document.querySelector('.ack-button-container');
        if (ackButtonContainer && data.target) {
            if (data.target.is_down && data.target.current_ack_token && !data.target.acknowledged_at) {
                const ackURL = '/api/acknowledge/' + data.target.current_ack_token;
                ackButtonContainer.innerHTML = '<a href="' + ackURL + '" class="ack-button ack-button-active">🔔 Acknowledge</a>';
            } else {
                ackButtonContainer.innerHTML = '<button class="ack-button ack-button-disabled" disabled>🔔 Acknowledge</button>';
            }
        }
        
        updateStatistics(history);
        updateChart(history);
        updateLogEntries(history);
        
    } catch (error) {
        console.error('Failed to update data:', error);
    }
}

function updateStatistics(history) {
    if (history.length === 0) return;
    
    // Calculate average page size
    let totalSize = 0;
    let validSizeCount = 0;
    for (const entry of history) {
        if (entry.Success && entry.ResponseSize > 0) {
            totalSize += entry.ResponseSize;
            validSizeCount++;
        }
    }
    const avgPageSize = validSizeCount > 0 ? totalSize / validSizeCount : 0;
    
    // Calculate p95 response time
    const successfulTimes = history.filter(e => e.Success).map(e => e.ResponseTime);
    successfulTimes.sort((a, b) => a - b);
    const p95Index = Math.floor(successfulTimes.length * 0.95);
    const p95ResponseTime = successfulTimes.length > 0 ? successfulTimes[Math.min(p95Index, successfulTimes.length - 1)] / 1000.0 : 0;
    
    // Update stat values
    const statCards = document.querySelectorAll('.stat-value');
    if (statCards.length >= 3) {
        // Average page size
        let avgSizeStr = 'N/A';
        if (avgPageSize > 0) {
            if (avgPageSize < 1024) {
                avgSizeStr = Math.floor(avgPageSize) + ' bytes';
            } else if (avgPageSize < 1024 * 1024) {
                avgSizeStr = (avgPageSize / 1024).toFixed(2) + ' KB';
            } else {
                avgSizeStr = (avgPageSize / (1024 * 1024)).toFixed(2) + ' MB';
            }
        }
        statCards[0].textContent = avgSizeStr;
        
        // P95 response time
        const p95Str = p95ResponseTime > 0 ? parseFloat(p95ResponseTime.toPrecision(3)) + 's' : 'N/A';
        statCards[1].textContent = p95Str;
        
        // Total checks
        statCards[2].textContent = history.length.toString();
    }
}

function updateChart(history) {
    const last100 = history.slice(-100);
    const newData = last100.map(entry => ({
        timestamp: new Date(entry.Timestamp).getTime(),
        success: entry.Success,
        responseTime: entry.ResponseTime,
        visualDifference: entry.VisualDifference || 0
    }));
    
    const newLabels = newData.map(d => {
        const date = new Date(d.timestamp);
        return date.toLocaleTimeString('en-US', { 
            hour: '2-digit', 
            minute: '2-digit', 
            second: '2-digit',
            hour12: false 
        });
    });
    
    chart.data.labels = newLabels;
    chart.data.datasets[0].data = newData.map(d => {
        if (!d.success) return null;
        return isPageComparison ? d.visualDifference : d.responseTime / 1000;
    });
    chart.data.datasets[0].pointBackgroundColor = newData.map(d => d.success ? '#3fb950' : '#f85149');
    chart.data.datasets[0].pointBorderColor = newData.map(d => d.success ? '#3fb950' : '#f85149');
    chart.data.datasets[0].pointHoverBackgroundColor = newData.map(d => d.success ? '#3fb950' : '#f85149');
    chart.data.datasets[0].segment = {
        borderColor: ctx => {
            const idx = ctx.p0DataIndex;
            const p0Success = newData[idx]?.success;
            const p1Success = newData[idx + 1]?.success;
            if (!p0Success || !p1Success) return '#f85149';
            return '#3fb950';
        }
    };
    chart.data.datasets[1].data = newData.map(d => !d.success ? (isPageComparison ? 100 : 0) : null);
    
    window.chartData = newData;
    chart.update();
}

function updateLogEntries(history) {
    const terminalBody = document.querySelector('.terminal-body');
    if (!terminalBody) return;
    
    const last100 = history.slice(-100);
    let newHTML = '';
    
    // Iterate in reverse order to show most recent at top
    for (let i = last100.length - 1; i >= 0; i--) {
        const entry = last100[i];
        const entryID = i + 1;
        
        // Build log entry
        let statusIcon = '✅';
        let statusClass = 'success';
        
        // Check if this is a warmup/baseline collection entry
        const isWarmup = entry.ResponseBody && entry.ResponseBody.includes('Warmup:');
        
        if (isWarmup) {
            statusIcon = 'ℹ️';
            statusClass = 'info';
        } else if (!entry.Success) {
            statusIcon = '❌';
            statusClass = 'error';
        }
        if (entry.WasRecovered) {
            statusIcon = '🔄';
            statusClass = 'recovered';
        }
        if (entry.WasAcked) {
            statusIcon = '🔔';
        }
        
        let statusText = '';
        if (isWarmup) {
            statusText = 'INFO';
        } else if (entry.Success) {
            const seconds = entry.ResponseTime / 1000.0;
            if (seconds === 0) {
                statusText = 'OK - 0s';
            } else {
                statusText = 'OK - ' + parseFloat(seconds.toPrecision(3)) + 's';
            }
        } else {
            statusText = 'FAILED';
        }
        
        let details = '';
        if (isWarmup) {
            details = entry.ResponseBody;
        } else {
            if (entry.StatusCode > 0) details += 'HTTP ' + entry.StatusCode + ' ';
            if (entry.ErrorMessage) details += entry.ErrorMessage + ' ';
            if (entry.AlertSent) details += 'Alert #' + entry.AlertCount + ' sent ';
            if (entry.WasAcked) details += 'Acknowledged ';
            if (entry.WasRecovered) details += 'Recovered';
        }
        
        // Build expanded content
        let expandedLines = [];
        const timestamp = new Date(entry.Timestamp);
        expandedLines.push('Timestamp: ' + timestamp.toLocaleString() + ' ' + (timestamp.toString().match(/\(([^)]+)\)$/)?.[1] || ''));
        if (entry.StatusCode > 0) expandedLines.push('Status Code: ' + entry.StatusCode);
        if (entry.Success) {
            const seconds = entry.ResponseTime / 1000.0;
            expandedLines.push('Response Time: ' + parseFloat(seconds.toPrecision(3)) + 's');
        }
        if (entry.ResponseSize > 0) {
            let sizeStr = '';
            if (entry.ResponseSize < 1024) {
                sizeStr = entry.ResponseSize + ' bytes';
            } else if (entry.ResponseSize < 1024 * 1024) {
                sizeStr = (entry.ResponseSize / 1024).toFixed(2) + ' KB';
            } else {
                sizeStr = (entry.ResponseSize / (1024 * 1024)).toFixed(2) + ' MB';
            }
            expandedLines.push('Response Size: ' + sizeStr);
        }
        if (entry.ContentType) expandedLines.push('Content-Type: ' + entry.ContentType);
        if (entry.VisualDifference > 0) expandedLines.push('Visual Difference: ' + entry.VisualDifference.toFixed(2) + '%');
        if (entry.ErrorMessage) expandedLines.push('Error: ' + entry.ErrorMessage);
        if (entry.AlertSent) expandedLines.push('Alert Sent: Yes (Alert #' + entry.AlertCount + ')');
        if (entry.WasAcked) expandedLines.push('Acknowledged: Yes');
        if (entry.WasRecovered) expandedLines.push('Status: Recovered');
        
        // Build expanded content
        let expandedContent = '';
        for (const line of expandedLines) {
            expandedContent += '<div>' + escapeHtml(line) + '</div>';
        }
        
        // Add response body if present
        if (entry.ResponseBody) {
            expandedContent += '<div></div>';
            expandedContent += '<div>Response Body:</div>';
            expandedContent += '<pre>' + escapeHtml(entry.ResponseBody) + '</pre>';
        }
        
        // Add screenshot images for page-comparison
        if (entry.ScreenshotPath || entry.DiffImagePath) {
            expandedContent += '<div style="margin-top: 12px; padding-top: 12px; border-top: 1px solid #30363d;"></div>';
            expandedContent += '<div style="font-weight: 600; margin-bottom: 8px;">📸 Visual Comparison:</div>';
            expandedContent += '<div style="display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin-top: 8px;">';
            
            if (entry.ScreenshotPath) {
                const filename = entry.ScreenshotPath.split('/').pop();
                expandedContent += '<div>';
                expandedContent += '<div style="font-size: 11px; color: #8b949e; margin-bottom: 4px;">Current Screenshot:</div>';
                expandedContent += '<a href="/api/screenshots/' + filename + '" target="_blank">';
                expandedContent += '<img src="/api/screenshots/' + filename + '" style="width: 100%; border: 1px solid #30363d; border-radius: 4px; cursor: pointer;" alt="Current screenshot" />';
                expandedContent += '</a>';
                expandedContent += '</div>';
            }
            
            if (entry.DiffImagePath) {
                const filename = entry.DiffImagePath.split('/').pop();
                expandedContent += '<div>';
                expandedContent += '<div style="font-size: 11px; color: #8b949e; margin-bottom: 4px;">Difference Overlay:</div>';
                expandedContent += '<a href="/api/screenshots/' + filename + '" target="_blank">';
                expandedContent += '<img src="/api/screenshots/' + filename + '" style="width: 100%; border: 1px solid #30363d; border-radius: 4px; cursor: pointer;" alt="Diff image" />';
                expandedContent += '</a>';
                expandedContent += '</div>';
            }
            
            expandedContent += '</div>';
            expandedContent += '<div style="font-size: 11px; color: #8b949e; margin-top: 8px; font-style: italic;">Click images to view full size</div>';
        }
        
        const isExpanded = expandedEntries.has(entryID);
        const expandClass = isExpanded ? ' expanded' : '';
        const displayStyle = isExpanded ? 'block' : 'none';
        
        const entryTime = timestamp.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
        
        newHTML += '<div class="log-entry-wrapper">';
        newHTML += '<div class="log-entry ' + statusClass + '" onclick="toggleEntry(' + entryID + ')">';
        newHTML += '<span class="log-expand' + expandClass + '">▶</span>';
        newHTML += '<span class="log-timestamp">' + entryTime + '</span>';
        newHTML += '<span class="log-icon">' + statusIcon + '</span>';
        newHTML += '<span class="log-status">' + statusText + '</span>';
        newHTML += '<span class="log-details">' + escapeHtml(details) + '</span>';
        newHTML += '</div>';
        newHTML += '<div id="entry-' + entryID + '" class="entry-expanded" style="display:' + displayStyle + ';">';
        newHTML += expandedContent;
        newHTML += '</div>';
        newHTML += '</div>';
    }
    
    if (newHTML) {
        terminalBody.innerHTML = newHTML;
    }
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Make chartData global for tooltip callbacks
window.chartData = chartData;

// Start auto-update
setInterval(updateData, 5000);
