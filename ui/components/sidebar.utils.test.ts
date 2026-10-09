import { describe, expect, it } from "vitest";
import { HIDDEN_UPSTREAM_PROMO_CARD_IDS, isUpstreamPromoCardHidden } from "./sidebar.utils";

describe("upstream sidebar promo suppression", () => {
	it("hides the release-upgrade card and the production-setup sales card", () => {
		expect([...HIDDEN_UPSTREAM_PROMO_CARD_IDS]).toEqual(["new-release", "production-setup"]);
		expect(isUpstreamPromoCardHidden("new-release")).toBe(true);
		expect(isUpstreamPromoCardHidden("production-setup")).toBe(true);
	});

	it("leaves operational cards visible", () => {
		expect(isUpstreamPromoCardHidden("setup-required")).toBe(false);
		expect(isUpstreamPromoCardHidden("restart-required")).toBe(false);
		expect(isUpstreamPromoCardHidden("onboarding-incomplete")).toBe(false);
	});
});