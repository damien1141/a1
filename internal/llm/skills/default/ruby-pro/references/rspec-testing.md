# RSpec & Rails Testing

## Spec types

| Type | What it tests |
|---|---|
| `type: :model` | Active Record validations, associations, scopes |
| `type: :request` | Full stack: routing → controller → view (HTML/JSON) |
| `type: :system` | Capybara + browser (Chrome headless) |
| `type: :job` | Active Job / Sidekiq jobs |
| `type: :mailer` | Mailers |
| `type: :lib` | Plain Ruby classes |

## Model spec

```ruby
require "rails_helper"

RSpec.describe Post, type: :model do
  describe "validations" do
    it { should validate_presence_of(:title) }
    it { should validate_length_of(:title).is_at_most(200) }
  end

  describe "associations" do
    it { should belong_to(:author).class_name("User") }
    it { should have_many(:comments).dependent(:destroy) }
  end

  describe "scopes" do
    let!(:old) { create(:post, :published, published_at: 2.days.ago) }
    let!(:new) { create(:post, :published, published_at: 1.day.ago) }

    it "returns published posts newest first" do
      expect(Post.published).to eq([new, old])
    end
  end
end
```

`shoulda-matchers` provides the one-line matchers (`validate_presence_of`, `belong_to`).

## Request spec

```ruby
require "rails_helper"

RSpec.describe "Posts API", type: :request do
  describe "GET /api/posts/:id" do
    let(:post) { create(:post, :published) }

    it "returns the post for authenticated users" do
      user = create(:user)
      get "/api/posts/#{post.id}", headers: auth_headers(user)

      expect(response).to have_http_status(:ok)
      expect(json["data"]["id"]).to eq(post.id)
    end

    it "returns 401 for anonymous users" do
      get "/api/posts/#{post.id}"
      expect(response).to have_http_status(:unauthorized)
    end
  end
end
```

Request specs hit the full stack — routing, middleware, controllers, views/serializers. Use over controller specs (deprecated in Rails 5+).

## System spec

```ruby
require "rails_helper"

RSpec.describe "Posts", type: :system do
  before { driven_by(:selenium_chrome_headless) }

  it "lets a user create a post" do
    user = create(:user, email: "a@b.c", password: "secret")
    visit new_user_session_path
    fill_in "Email", with: "a@b.c"
    fill_in "Password", with: "secret"
    click_button "Log in"

    visit new_post_path
    fill_in "Title", with: "Hello"
    fill_in "Body", with: "World"
    click_button "Create Post"

    expect(page).to have_text("Post created")
    expect(page).to have_text("Hello")
  end
end
```

System specs use Capybara + a real browser. They catch JS-driven behavior. Slow — use sparingly for critical flows.

## FactoryBot

```ruby
# spec/factories/users.rb
FactoryBot.define do
  factory :user do
    sequence(:email) { |n| "user#{n}@example.com" }
    password { "secret123" }
    name { "Alice" }

    trait :admin do
      role { "admin" }
    end
  end
end

# spec/factories/posts.rb
FactoryBot.define do
  factory :post do
    title { "Title" }
    body { "Body" }
    author { association :user }
    status { :draft }

    trait :published do
      status { :published }
      published_at { 1.day.ago }
    end
  end
end

# Usage
user = create(:user, :admin)
post = create(:post, :published, author: user)
```

- `build` doesn't persist; `create` does
- `build_stubbed` fakes persistence (no DB hit) for fast unit tests
- Use traits (`:admin`, `:published`) over inheritance

## Mocking

```ruby
RSpec.describe PaymentService, type: :lib do
  let(:gateway) { double("PaymentGateway") }
  subject(:service) { described_class.new(gateway: gateway) }

  it "charges the gateway" do
    expect(gateway).to receive(:charge).with(amount: 100).and_return(success: true)
    service.charge(100)
  end

  it "handles gateway failure" do
    allow(gateway).to receive(:charge).and_raise(GatewayError)
    expect { service.charge(100) }.to raise_error(PaymentFailed)
  end
end
```

Prefer dependency injection over `allow(PaymentGateway).to receive(:charge)`. RSpec mocks are fine; `Mocha` and `instance_double` are alternatives.

## Testing Sidekiq jobs

```ruby
require "rails_helper"

RSpec.describe SendWelcomeEmailJob, type: :job do
  include ActiveJob::TestHelper

  it "sends the welcome email" do
    user = create(:user)
    expect { described_class.perform_later(user.id) }.to have_enqueued_job.with(user.id)
  end

  it "is idempotent" do
    user = create(:user)
    allow(UserMailer).to receive_message_chain(:welcome, :deliver_now)

    2.times { described_class.perform_now(user.id) }
    expect(UserMailer).to have_received(:welcome).once
  end
end
```

`have_enqueued_job` (ActiveJob) for enqueue assertions. `perform_now` for synchronous execution in tests. Sidekiq has its own testing helpers (`Sidekiq::Testing.inline!`).

## Coverage with SimpleCov

```ruby
# spec/spec_helper.rb
require "simplecov"
SimpleCov.start "rails" do
  add_filter "/spec/"
  add_filter "/config/"
  minimum_coverage 85
  minimum_coverage_by_file 60
end
```

`minimum_coverage` raises if coverage drops below threshold — fails CI.

## Common pitfalls

- `let` is lazy — `let!(:x)` to execute eagerly; useful for setup that must run
- `before(:all)` shared state across examples — flaky; prefer `before(:each)`
- `expect(...).to receive(...)` not called — test fails; check spelling
- `allow(...)` without `expect` — silent; fine for stubs
- `Time.now` in tests — use `Time.current` + `travel_to` for freezing
- Capybara without `driven_by` in system specs — defaults to rack_test (no JS)
- FactoryBot `create` for read-only tests — `build_stubbed` is faster
- `expect(response.body).to include(...)` for JSON — use `JSON.parse(response.body)` and `expect(json).to include(...)`
