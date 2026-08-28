# Onboarding & KYC requirements

New customers enter the bank through a **public, three-step KYC onboarding journey**. The application is submitted, then reviewed by an administrator. The account and its opening event exist only after approval.

## Submission — `POST /onboarding`
- Transport: `multipart/form-data` with `payload` (JSON profile) and `passport_image` (required, ≤5 MB, JPEG/PNG/WebP).
- Public endpoint: no session required; returns `201` with `{customer_id, kyc_status: "waiting_for_approval"}`.

### Field validation
- **Required** fields: legal first/last name, date of birth, nationality, email, phone, address line1/city/postal code/country, government document type/number/issuing country.
- **Password**: 10–128 characters; stored as `pbkdf2_sha256` (600,000 iterations), never in plaintext.
- **Email**: valid RFC address, normalized lowercased.
- **Phone**: E.164 format (`+1-9` followed by 7–14 digits).
- **Country codes** (nationality, address country, document issuing country): exactly two uppercase letters.
- **Date of birth**: valid past date; computed age must be ≥ 18.
- **Government document type**: one of `passport`, `national_id`, `driver_license`.
- **Initial deposit**: zero or ≥ 10.00 minor-unit cents (10.00 = 1000).
- Field values are trimmed; nationality/document fields uppercased; email lowercased.

### Uniqueness
- A normalized email or a matching government-document tuple (type + number + issuing country) cannot onboard twice; duplicates return `409` (`ErrCustomerAlreadyExists`).
- Passport images are stored in the private MinIO bucket `odtbank-passports` (Postgres path) under key `passports/{customer_id}`; the image is **never** in event payloads and is served only through the authenticated admin endpoint.

## State machine
`waiting_for_approval` → (approve) → `approved` | (reject) → `rejected`. Approval and rejection are terminal; a second review returns `409` (`ErrApplicationReviewed`).

## Approval — admin
- `POST /admin/applications/{customer_id}/approve` creates the account and its `AccountOpened` event exactly once, funding it with the requested initial deposit.
- `POST /admin/applications/{customer_id}/reject` requires a reason (1–500 chars, non-empty after trim); stores the terminal rejection reason returned to the customer on login.
- Waiting/rejected customers cannot use banking endpoints; rejection exposes the final reason via `GET /me`.

## Limits / non-goals
- KYC decisions are manual demo decisions; no identity provider or sanctions screening.
- KYC fields and passport objects are stored without application-level encryption.
