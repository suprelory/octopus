import { type PendingJump, type SiteChannelJumpTarget } from '@/stores/jump';

export type SiteChannelPendingJump = PendingJump & { target: SiteChannelJumpTarget };
