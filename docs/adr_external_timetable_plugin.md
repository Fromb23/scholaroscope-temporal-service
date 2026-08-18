# ADR: Temporal Service as External Timetable Plugin

Status: Accepted for implementation

Date: 2026-08-18

## Decision

The temporal service remains an independent Go/PostgreSQL service that integrates with Scholaroscope through signed, versioned plugin contracts. It is not collapsed into Django, does not use MySQL, and keeps UUID identifiers for temporal-owned entities.

```text
Scholaroscope kernel
    <-> signed, versioned integration protocol
Temporal timetable plugin
```

## Domain ownership

Temporal owns timetable calendars, bell periods, timetable versions, timetable entries, teacher availability projections, rooms, resources, scheduling constraints, conflict detection, publication, examination sittings, and timetable-specific scheduling decisions.

Scholaroscope owns identity, workspaces, memberships, workspace permissions, academic structure, teaching assignments, learners, notifications, sessions, lesson plans, plugin installation, and entitlement. Temporal receives only mapped UUID projections and trusted launch/event state.

## Identity mapping

Temporal records use UUIDs. Scholaroscope integer IDs remain in Scholaroscope and are translated through integration mappings. Temporal stores the external workspace/installation relationship and never treats raw Scholaroscope integer IDs as domain primary identities.

A temporal workspace maps to exactly one Scholaroscope workspace installation. Every tenant-owned temporal row carries the temporal workspace UUID and all queries/mutations are scoped to the authenticated temporal workspace.

## Launch and sessions

Temporal does not provide direct username/password authentication for managers. A manager arrives through a Scholaroscope-issued single-use grant. Temporal validates issuer, audience, HMAC/signature, expiry, nonce, installation mapping, and one-time consumption before creating an HttpOnly, Secure, SameSite-scoped portal session.

Portal sessions expire automatically and are invalidated by logout, plugin disablement, installation revocation, authorization revocation, or expiry. Critical publication operations must require current authorization by short TTL and/or Scholaroscope introspection.

## Webhooks

Incoming and outgoing webhooks use the shared envelope in `docs/integration-contracts.md`, installation-scoped signing secrets, timestamp skew checks, replay protection, durable inbox/outbox storage, at-least-once delivery, idempotent processing, bounded exponential backoff, dead-letter status, replay tooling, and correlation IDs.

## Timetable lifecycle

Draft versions may change. Published versions are immutable. Publication is atomic and may occur only when hard conflicts are resolved and the portal session has `timetable.publish`. Only publication or a published amendment changes Scholaroscope operational timetable truth.

## Examination timetable

Examination timetables use the same integration protocol and authorization model with examination-specific constraints, publication events, printable projections, and learning-session displacement reporting.

## Failure and recovery

Arbitrary database errors are not classified as ordinary slot conflicts. Domain validation returns structured conflict diagnostics; database constraints remain the concurrency barrier. Missed events are repaired by idempotent reconciliation from Scholaroscope kernel truth. Secrets and complete launch tokens are never logged.

## Stage 0 audit gap

The current service exposes unauthenticated URL-scoped endpoints such as `/orgs/{orgId}/calendar` and `/orgs/{orgId}/sessions/{sessionId}/schedule`. It generates fixed-duration slots from operating hours and breaks, has no migrations in the repository, lacks explicit bell periods, lacks launch/session auth, lacks signed webhook verification, lacks inbox/outbox persistence, lacks publication/version immutability, lacks rooms/resources/exams, lacks a Next.js management portal, and lacks automated tests.
