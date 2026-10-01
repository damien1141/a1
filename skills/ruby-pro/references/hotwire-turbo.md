# Hotwire / Turbo

## Three pillars

- **Turbo Drive** — intercepts link clicks and form submits; replaces `<body>` with the response. No full page reload.
- **Turbo Frames** — decompose the page into independent frames; navigating within a frame updates only that frame.
- **Turbo Streams** — server pushes partial updates (append/prepend/remove/replace) to the page, via form responses or WebSocket.

## Turbo Frame — partial page update

```erb
<%# app/views/posts/index.html.erb %>
<%= turbo_frame_tag "posts" do %>
  <%= render @posts %>
  <%= link_to "New Post", new_post_path, class: "btn" %>
<% end %>
```

```erb
<%# app/views/posts/_post.html.erb %>
<%= turbo_frame_tag dom_id(post) do %>
  <h2><%= post.title %></h2>
  <%= link_to "Edit", edit_post_path(post) %>
<% end %>
```

Clicking "Edit" inside a frame loads the edit form into that frame (not the whole page). Submitting the form returns the updated `_post` partial.

## Turbo Stream — server-driven updates

```erb
<%# app/views/posts/create.turbo_stream.erb %>
<%= turbo_stream.prepend "posts", @post %>
<%= turbo_stream.replace "new_post", partial: "posts/form", locals: { post: Post.new } %>
<%= turbo_stream.flash :success, "Post created" %>
```

Respond to a POST with both HTML and Turbo Stream:

```ruby
# app/controllers/posts_controller.rb
def create
  @post = Post.new(post_params)
  if @post.save
    respond_to do |format|
      format.html { redirect_to @post, notice: "Created." }
      format.turbo_stream  # renders create.turbo_stream.erb
    end
  else
    render :new, status: :unprocessable_entity
  end
end
```

## Turbo Stream actions

| Action | Effect |
|---|---|
| `append` | Insert at end of target's children |
| `prepend` | Insert at start of target's children |
| `replace` | Replace target element |
| `update` | Replace target's children only |
| `remove` | Remove target element |
| `before` | Insert before target |
| `after` | Insert after target |

```erb
<%= turbo_stream.remove dom_id(@comment) %>
<%= turbo_stream.replace dom_id(@post), @post %>
<%= turbo_stream.append "messages", partial: "messages/message", locals: { message: @msg } %>
```

## Broadcasting from jobs / models

```ruby
class Comment < ApplicationRecord
  belongs_to :post
  after_create_commit -> { broadcast_append_to(post, partial: "comments/comment") }
  after_destroy_commit -> { broadcast_remove_to(post) }
end

# In a view anywhere:
<%= turbo_stream_from @post %>
<%= turbo_frame_tag "comments" do %>
  <%= render @post.comments %>
<% end %>
```

`turbo_stream_from @post` opens a WebSocket (Action Cable) subscribed to the `post` channel. Any `broadcast_*` to that channel updates every subscribed client in real time.

## Broadcast helpers

| Helper | Effect |
|---|---|
| `broadcast_append_to` | Append to all subscribers |
| `broadcast_prepend_to` | Prepend to all subscribers |
| `broadcast_replace_to` | Replace an element |
| `broadcast_remove_to` | Remove an element |
| `broadcast_action_to` | Custom action (e.g. `action: :morph`) |

## Stimulus — modest JS

```html
<div data-controller="counter">
  <span data-counter-target="display">0</span>
  <button data-action="click->counter#inc">+</button>
</div>

<script>
// app/javascript/controllers/counter_controller.js
import { Controller } from "@hotwired/stimulus"

export default class extends Controller {
  static targets = ["display"]
  inc() { this.displayTarget.textContent = Number(this.displayTarget.textContent) + 1 }
}
</script>
```

Stimulus is the JS side of Hotwire. Tiny controllers; HTML declares the wiring via `data-*` attributes.

## Turbo Morphing (Rails 7.2+)

```erb
<%# Refresh the page in place, preserving form state %>
<%= turbo_refreshes_with method: :morph, scroll: :preserve %>
```

Morph replaces the page content while preserving focus and scroll — better UX than full Turbo Drive refreshes.

## Lazy-loaded frames

```erb
<%= turbo_frame_tag "comments", src: post_comments_path(@post), loading: "lazy" do %>
  Loading comments...
<% end %>
```

`src:` makes the frame lazy — loads when scrolled into view (with `loading: "lazy"`).

## Common pitfalls

- Turbo Drive breaks non-GET forms — use `data-turbo="false"` to opt out per-link or per-form
- `<form>` without `data-turbo="true"` for streaming — default in Rails 7
- Forgetting `status: :unprocessable_entity` on validation failure — Turbo doesn't show errors
- Turbo Streams without a matching `turbo_stream_from` — updates don't reach the client
- `broadcast_*` from `after_save` (not `_commit`) — fires before commit, race with other workers
- Action Cable without Redis in production — `cable.yml` needs Redis adapter
- Stimulus controller not registered — import it in `controllers/index.js`
- Morphing with persistent state — reset in `stimulus:disconnect` if needed
