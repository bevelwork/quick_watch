# Web Assets Structure

This directory contains extracted CSS, JavaScript, and HTML templates for Quick Watch's web interface.

## Directory Structure

```
web/
├── css/
│   ├── target_list.css       # Styles for the target list page
│   └── target_detail.css      # Styles for the target detail page
├── js/
│   ├── target_list.js         # JavaScript for target list functionality
│   └── target_detail.js       # JavaScript for target detail page (charts, logs, etc.)
└── templates/
    ├── acknowledge_form.html  # Acknowledgement form template
    └── target_list.html       # Target list page template stub
```

## Usage

### CSS Files
- `target_list.css`: Contains all styling for the targets overview page
- `target_detail.css`: Contains all styling for individual target detail views

### JavaScript Files  
- `target_list.js`: Handles filtering and auto-refresh for target list
- `target_detail.js`: Manages charts (Chart.js), log streaming, pause/resume, and auto-updates

### Templates
- HTML templates use Go's `html/template` package
- Data is passed via template variables (`.FieldName`)
- JavaScript receives runtime data via data attributes on HTML elements

## Implementation Notes

### Hybrid Approach
This refactoring uses a hybrid approach:
- ✅ **Complex pages** (target list, target detail) have extracted CSS/JS/HTML
- ⏭️ **Simple pages** (acknowledge forms, error pages) remain inline in server.go

### Next Steps to Complete Integration

1. **Add HTTP file server in server.go:**
   ```go
   mux.Handle("/web/", http.StripPrefix("/web/", http.FileServer(http.Dir("./web"))))
   ```

2. **Load templates using html/template:**
   ```go
   import "html/template"
   
   var templates = template.Must(template.ParseGlob("web/templates/*.html"))
   ```

3. **Update handlers to use external files:**
   - Modify `handleTargetList` to load `target_list.html` 
   - Modify `handleTargetDetail` to load template with external CSS/JS
   - Pass data via data attributes for JavaScript

4. **Test both pages render correctly**

## Benefits

- **Maintainability**: Separate concerns (structure, style, behavior)
- **Development**: Easier to edit CSS/JS without escaping issues
- **Performance**: Browser can cache static assets
- **Tooling**: Can use standard CSS/JS linters and formatters

