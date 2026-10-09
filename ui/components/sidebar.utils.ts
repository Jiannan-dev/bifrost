/**
 * Fork (Jiannan-dev/bifrost): hide Maxim's sidebar sales card and the
 * upstream release-upgrade card. There is no config.json switch for either.
 *
 * Operational cards stay visible: setup lock, restart required, and the
 * incomplete onboarding resume card. See AGENTS.md "Sidebar upstream promo cards".
 */
export const HIDDEN_UPSTREAM_PROMO_CARD_IDS = ["new-release", "production-setup"] as const;

export function isUpstreamPromoCardHidden(cardId: string): boolean {
	return (HIDDEN_UPSTREAM_PROMO_CARD_IDS as readonly string[]).includes(cardId);
}