// Filter functionality
let filterTimeout;

function filterTargets() {
    const filterValue = document.getElementById('filterInput').value.toLowerCase();
    const cards = document.querySelectorAll('.target-card');
    let visibleCount = 0;
    
    cards.forEach(card => {
        const name = card.getAttribute('data-target-name');
        const url = card.getAttribute('data-target-url');
        
        if (name.includes(filterValue) || url.includes(filterValue)) {
            card.classList.remove('hidden');
            visibleCount++;
        } else {
            card.classList.add('hidden');
        }
    });
    
    // Update count
    const filterCount = document.getElementById('filterCount');
    if (filterValue) {
        filterCount.textContent = visibleCount + ' of ' + cards.length + ' targets';
        filterCount.style.display = 'inline';
    } else {
        filterCount.style.display = 'none';
    }
}

function clearFilter() {
    document.getElementById('filterInput').value = '';
    filterTargets();
    document.getElementById('filterInput').focus();
}

// Auto-refresh every 5 seconds (but don't reload if filtering)
setTimeout(() => {
    const filterValue = document.getElementById('filterInput').value;
    if (!filterValue) {
        window.location.reload();
    } else {
        // If filtering, just refresh after clearing filter
        setTimeout(() => window.location.reload(), 5000);
    }
}, 5000);

