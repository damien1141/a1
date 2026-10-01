# htmx Server-Side Integration

## Core principle

htmx servers return **HTML fragments**, not JSON. The response is swapped directly into the DOM. This is the fundamental difference from SPA/JSON API architecture.

```
Browser → HTTP Request (with htmx headers) → Server
Server → HTML Fragment Response (with optional htmx headers) → Browser
Browser → Swaps HTML into DOM
```

## Request headers (sent by htmx)

| Header | Value | Usage |
|--------|-------|-------|
| `HX-Request` | `"true"` | Detect htmx requests |
| `HX-Trigger` | Element ID | ID of triggering element |
| `HX-Trigger-Name` | Element name | Name attribute |
| `HX-Target` | Element ID | ID of target element |
| `HX-Current-URL` | URL | Current page URL |
| `HX-Boosted` | `"true"` | Present if via `hx-boost` |
| `HX-History-Restore-Request` | `"true"` | Restoring from history cache miss |
| `HX-Prompt` | User input | Response to `hx-prompt` dialog |

### Server-side detection

```python
# Python (Flask)
if request.headers.get('HX-Request'):
    return render_template('partial.html')
return render_template('full_page.html')
```

```js
// Node (Express)
if (req.headers['hx-request']) {
  return res.render('partial');
}
return res.render('full_page');
```

```java
// Java (Spring)
@GetMapping("/items")
public String items(@RequestHeader(value = "HX-Request", required = false) String hxRequest) {
  return "true".equals(hxRequest) ? "items :: list" : "items";
}
```

```go
// Go
if r.Header.Get("HX-Request") == "true" {
  tmpl.ExecuteTemplate(w, "list", items)
} else {
  tmpl.ExecuteTemplate(w, "page", items)
}
```

## Response headers

### `HX-Trigger` — server → client events

Simple event:
```
HX-Trigger: myEvent
```

Multiple:
```
HX-Trigger: event1, event2
```

Events with data (JSON):
```
HX-Trigger: {"showMessage": {"level": "info", "message": "Item saved!"}, "itemCount": "5"}
```

Timing variants:
- `HX-Trigger` — fires immediately on response receipt.
- `HX-Trigger-After-Swap` — fires after DOM swap.
- `HX-Trigger-After-Settle` — fires after settling.

Listening:
```html
<div hx-trigger="showMessage from:body" hx-get="/notifications" hx-target="this"></div>
```

Or via JS:
```js
document.body.addEventListener('showMessage', (e) => alert(e.detail.value));
```

### `HX-Location` — client-side redirect (no full reload)

```
HX-Location: /new-page
```

With options (JSON):
```
HX-Location: {"path": "/new-page", "target": "#content", "swap": "innerHTML"}
```

### `HX-Push-Url` — push URL to history

```
HX-Push-Url: /items/42
HX-Push-Url: false
```

### `HX-Replace-Url` — replace current URL (no new history entry)

```
HX-Replace-Url: /items/42
```

### `HX-Redirect` — full page redirect

```
HX-Redirect: /login
```

### `HX-Refresh` — full page refresh

```
HX-Refresh: true
```

### `HX-Reswap` — override swap strategy

```
HX-Reswap: outerHTML
```

### `HX-Retarget` — override swap target

```
HX-Retarget: #error-container
```

### `HX-Reselect` — override `hx-select`

```
HX-Reselect: #content
```

**Important:** Response headers are NOT processed on 3xx redirect responses.

## Response handling by status code

| Status | Swap? | Error event? |
|--------|-------|--------------|
| 2xx | Yes | No |
| 204 No Content | No | No |
| 3xx | Browser redirect | N/A |
| 4xx | No | Yes |
| 5xx | No | Yes |

### Custom configuration

```html
<meta name="htmx-config" content='{
  "responseHandling": [
    {"code": "204", "swap": false},
    {"code": "[23]..", "swap": true},
    {"code": "422", "swap": true, "error": true},
    {"code": "[45]..", "swap": false, "error": true}
  ]
}'>
```

### Swap 422 validation errors

Many APIs return `422 Unprocessable Entity` for validation. To swap the error HTML:

**Option 1:** Global config (above).

**Option 2:** `htmx:beforeSwap` event:
```js
document.body.addEventListener('htmx:beforeSwap', (evt) => {
  if (evt.detail.xhr.status === 422) {
    evt.detail.shouldSwap = true;
    evt.detail.isError = false;
  }
});
```

**Option 3:** `response-targets` extension:
```html
<form hx-post="/submit" hx-target-422="#errors">
```

### DELETE response pattern

- `200` + empty body → target removed.
- `204 No Content` → NO swap, element stays.

## Common server patterns

### Return HTML fragments

```python
# Flask
@app.route('/contacts/<int:id>', methods=['GET'])
def get_contact(id):
    contact = Contact.query.get_or_404(id)
    return render_template('contact_detail.html', contact=contact)
    # Returns: <div>Joe Smith — joe@example.com</div>
```

### Out-of-band updates

Return extra HTML that updates other parts of the page:

```python
@app.route('/contacts', methods=['POST'])
def create_contact():
    contact = Contact.create(request.form)
    return f"""
        <form hx-post="/contacts">…</form>
        <tr id="contact-{contact.id}" hx-swap-oob="beforeend:#contacts-table">
            <td>{contact.name}</td>
        </tr>
    """
```

### Trigger events from server

```python
@app.route('/contacts', methods=['POST'])
def create_contact():
    contact = Contact.create(request.form)
    response = make_response(render_template('contact_form.html'))
    response.headers['HX-Trigger'] = json.dumps({
        'contactCreated': {'id': contact.id},
        'showMessage': 'Contact created!',
    })
    return response
```

### Conditional full page vs fragment

```python
@app.route('/page')
def page():
    if request.headers.get('HX-Request'):
        return render_template('page_content.html')
    return render_template('page_full.html')
```

### Redirect after form submission

```python
@app.route('/login', methods=['POST'])
def login():
    if authenticate(request.form):
        response = make_response('')
        response.headers['HX-Redirect'] = '/dashboard'
        return response
    return render_template('login_error.html'), 422
```

## Framework examples

### Python — Django

```python
from django.http import HttpResponse
from django.template.loader import render_to_string

def contact_list(request):
    contacts = Contact.objects.all()
    if request.headers.get('HX-Request'):
        html = render_to_string('contacts/_list.html', {'contacts': contacts})
        return HttpResponse(html)
    return render(request, 'contacts/index.html', {'contacts': contacts})

def delete_contact(request, pk):
    Contact.objects.filter(pk=pk).delete()
    return HttpResponse('')   # empty 200 = remove element
```

### Python — Flask

```python
@app.route('/search', methods=['POST'])
def search():
    query = request.form.get('q', '')
    results = Contact.query.filter(Contact.name.ilike(f'%{query}%')).all()
    return render_template('_search_results.html', results=results)
```

### Python — FastAPI

```python
from fastapi import Request, Response
from fastapi.responses import HTMLResponse

@app.post("/contacts", response_class=HTMLResponse)
async def create_contact(request: Request):
    form = await request.form()
    contact = await Contact.create(**form)
    return templates.TemplateResponse("_contact_row.html", {"request": request, "contact": contact})
```

### JavaScript — Express

```js
app.get('/contacts', (req, res) => {
  const contacts = getContacts();
  if (req.headers['hx-request']) {
    return res.render('contacts/list', { contacts });
  }
  res.render('contacts/index', { contacts });
});

app.delete('/contacts/:id', (req, res) => {
  deleteContact(req.params.id);
  res.status(200).send('');   // empty 200
});
```

### Java — Spring Boot

```java
@Controller
public class ContactController {
  @GetMapping("/contacts")
  public String list(Model model, @RequestHeader(value = "HX-Request", required = false) String htmx) {
    model.addAttribute("contacts", contactRepo.findAll());
    return htmx != null ? "contacts :: list" : "contacts";
  }

  @DeleteMapping("/contacts/{id}")
  public ResponseEntity<String> delete(@PathVariable Long id) {
    contactRepo.deleteById(id);
    return ResponseEntity.ok("");
  }
}
```

### Go

```go
func contactsHandler(w http.ResponseWriter, r *http.Request) {
  contacts := getContacts()
  if r.Header.Get("HX-Request") == "true" {
    tmpl.ExecuteTemplate(w, "contacts-list", contacts)
  } else {
    tmpl.ExecuteTemplate(w, "contacts-page", contacts)
  }
}

func deleteHandler(w http.ResponseWriter, r *http.Request) {
  deleteContact(r.URL.Query().Get("id"))
  w.WriteHeader(http.StatusOK)  // empty 200
}
```

### Ruby — Rails

```ruby
class ContactsController < ApplicationController
  def index
    @contacts = Contact.all
    if request.headers["HX-Request"]
      render partial: "contacts/list", locals: { contacts: @contacts }
    else
      render :index
    end
  end

  def destroy
    Contact.find(params[:id]).destroy
    head :ok   # empty 200
  end
end
```

### PHP — Laravel

```php
public function index(Request $request) {
  $contacts = Contact::all();
  if ($request->header('HX-Request')) {
    return view('contacts._list', compact('contacts'));
  }
  return view('contacts.index', compact('contacts'));
}

public function destroy(Contact $contact) {
  $contact->delete();
  return response('');  // empty 200
}
```

## Quirks & gotchas

### GET on non-form elements excludes form values

```html
<!-- ❌ Button does NOT include the input's value -->
<form>
  <input name="query" value="test">
  <button hx-get="/search">Search</button>
</form>

<!-- ✅ Include the form explicitly -->
<button hx-get="/search" hx-include="closest form">Search</button>
```

### `204 No Content` = no swap

For DELETE operations that should remove an element, return `200` with empty body. `204` causes htmx to do nothing.

### Body targeting always uses innerHTML

`hx-swap="outerHTML"` on `<body>` is automatically converted to `innerHTML`. You cannot replace the `<body>` element itself.

### `hx-boost` caveats

- Does not push URL for forms by default (only anchors).
- Can break scripts that expect full page load.
- Head tags need `head-support` extension to be managed properly.

### Loading htmx asynchronously

htmx expects to be loaded via a blocking `<script>` tag. Using `type="module"`, `defer`, or dynamic import can cause initialization issues where htmx misses elements already in the DOM.

### Attribute inheritance surprises

```html
<div hx-target="#output">           <!-- All children inherit this target -->
  <button hx-get="/a">Uses #output</button>
  <button hx-get="/b">Uses #output too — maybe unintended?</button>
</div>
```

Disable globally with `htmx.config.disableInheritance = true`, then opt-in per element with `hx-inherit`.

### History cache conflicts with third-party JS

Libraries that modify the DOM (charts, rich text editors) may not restore properly from htmx's localStorage history cache. Solutions:
- `htmx.config.historyCacheSize = 0`
- `hx-history="false"` on affected pages
- Re-initialize libraries via `htmx:historyRestore` event

## Quick Reference

| Concern | Pattern |
|---------|---------|
| Detect htmx request | `HX-Request: true` header |
| Return fragment | Render partial template instead of full page |
| DELETE | `200` + empty body (NOT `204`) |
| Server→client event | `HX-Trigger` header (JSON for data) |
| History update | `HX-Push-Url` / `HX-Replace-Url` |
| Redirect | `HX-Redirect` (full reload) / `HX-Location` (AJAX) |
| Validation error | `422` + `response-targets` ext OR `htmx:beforeSwap` |
| Multi-target update | `hx-swap-oob` in response |
| Conditional rendering | Check `HX-Request` header server-side |
| Avoid 204 for DELETE | Use `200` empty |
