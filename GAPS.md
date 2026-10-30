# Gaps found while implementing from the vectors

None yet that disagree with a vector. If a vector and `spec.md` diverge, file an issue on `aimarket-protocol` (`history: live`) and record it here — do not silently match the reference hub.

`POST /ai-market/v2/channel/open` and `channel/close` are not implemented and must not be called: open transfers tokens into escrow (§6.1).
