'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ChevronDown, Pencil, Pin, PinOff, Trash2, Waves, X } from 'lucide-react';
import { AnimatePresence, motion } from 'motion/react';
import { useTranslations } from 'next-intl';
import {
    GroupMode,
    type Group,
    type GroupUpdateRequest,
    useDeleteGroup,
    useToggleGroupPin,
    useUpdateGroup,
} from '@/api/endpoints/group';
import { useModelChannelList } from '@/api/endpoints/model';
import { CopyIconButton } from '@/components/common/CopyButton';
import { toast } from '@/components/common/Toast';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import {
    MorphingDialog,
    MorphingDialogClose,
    MorphingDialogContainer,
    MorphingDialogContent,
    MorphingDialogDescription,
    MorphingDialogTitle,
    MorphingDialogTrigger,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { cn } from '@/lib/utils';
import { OVERLAY_ENTRANCE_FROM_RIGHT } from '@/lib/animations/css-entrances';
import { getGroupIcon } from '@/lib/model-icons';
import { GroupEditor, type GroupEditorValues } from './Editor';
import { MemberList, type SelectedMember } from './ItemList';
import { PresetPopover } from './PresetPopover';
import { MODE_LABELS, modelChannelKey } from './utils';

interface EditDialogContentProps {
    group: Group;
    displayMembers: SelectedMember[];
    isSubmitting: boolean;
    onSubmit: (values: GroupEditorValues, onDone?: () => void) => void;
}

function EditDialogContent({ group, displayMembers, isSubmitting, onSubmit }: EditDialogContentProps) {
    const { setIsOpen } = useMorphingDialog();
    const t = useTranslations('group');

    return (
        <div className="relative flex h-full min-h-0 w-full max-w-full flex-col">
            <MorphingDialogTitle className="shrink-0">
                <header className="relative mb-4 flex items-start justify-between gap-4">
                    <div className="space-y-3">
                        <div className="inline-flex items-center gap-2 rounded-full border border-primary/15 bg-card px-3 py-1 text-[0.68rem] font-semibold text-primary">
                            <Waves className="size-3.5" />
                            {t('detail.actions.edit')}
                        </div>
                        <div className="space-y-1">
                            <h2 className="text-2xl font-bold text-card-foreground">{t('detail.actions.edit')}</h2>
                            <p className="text-sm text-muted-foreground">{group.name}</p>
                        </div>
                    </div>
                    <MorphingDialogClose className="relative right-0 top-0" />
                </header>
            </MorphingDialogTitle>
            <MorphingDialogDescription className="relative flex-1 min-h-0 overflow-hidden">
                <GroupEditor
                    key={`edit-group-${group.id}`}
                    initial={{
                        name: group.name,
                        match_regex: group.match_regex ?? '',
                        mode: group.mode,
                        first_token_time_out: group.first_token_time_out ?? 0,
                        session_keep_time: group.session_keep_time ?? 0,
                        retry_enabled: group.retry_enabled ?? false,
                        max_retries: group.max_retries ?? 3,
                        members: displayMembers,
                    }}
                    submitText={t('detail.actions.save')}
                    submittingText={t('create.submitting')}
                    isSubmitting={isSubmitting}
                    onCancel={() => setIsOpen(false)}
                    onSubmit={(values) => onSubmit(values, () => setIsOpen(false))}
                />
            </MorphingDialogDescription>
        </div>
    );
}

export function GroupCard({ group }: { group: Group }) {
    const t = useTranslations('group');
    const updateGroup = useUpdateGroup();
    const deleteGroup = useDeleteGroup();
    const togglePin = useToggleGroupPin();
    const { data: modelChannels = [] } = useModelChannelList();

    const [expanded, setExpanded] = useState(false);
    const [confirmDelete, setConfirmDelete] = useState(false);
    const [isDragging, setIsDragging] = useState(false);
    const [members, setMembers] = useState<SelectedMember[]>([]);
    const [weightOverrides, setWeightOverrides] = useState<Record<string, number>>({});
    const weightTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const membersRef = useRef<SelectedMember[]>([]);

    const modelChannelByKey = useMemo(() => {
        const map = new Map<string, (typeof modelChannels)[number]>();
        modelChannels.forEach((modelChannel) => {
            map.set(modelChannelKey(modelChannel.channel_id, modelChannel.name), modelChannel);
        });
        return map;
    }, [modelChannels]);

    const displayMembers = useMemo(
        (): SelectedMember[] =>
            [...(group.items ?? [])]
                .sort((left, right) => left.priority - right.priority)
                .map((item) => {
                    const key = modelChannelKey(item.channel_id, item.model_name);
                    const modelChannel = modelChannelByKey.get(key);
                    return {
                        ...modelChannel,
                        id: key,
                        name: item.model_name,
                        enabled: modelChannel?.enabled ?? true,
                        channel_id: item.channel_id,
                        channel_name: modelChannel?.channel_name ?? `Channel ${item.channel_id}`,
                        item_id: item.id,
                        weight: item.weight,
                    };
                }),
        [group.items, modelChannelByKey],
    );

    const effectiveDisplayMembers = useMemo(
        () =>
            displayMembers.map((member) => {
                const weight = weightOverrides[member.id];
                return weight === undefined ? member : { ...member, weight };
            }),
        [displayMembers, weightOverrides],
    );

    const renderedMembers = useMemo(
        () => (isDragging ? members : effectiveDisplayMembers),
        [effectiveDisplayMembers, isDragging, members],
    );

    const groupIcon = useMemo(
        () => getGroupIcon(group.name, displayMembers.map((member) => member.name)),
        [displayMembers, group.name],
    );
    const GroupAvatar = groupIcon?.Avatar;
    const firstModelName = displayMembers[0]?.name ?? '';
    const needsScroll = renderedMembers.length >= 4;

    useEffect(() => {
        membersRef.current = renderedMembers;
    }, [renderedMembers]);

    useEffect(
        () => () => {
            if (weightTimerRef.current) clearTimeout(weightTimerRef.current);
        },
        [],
    );

    const onSuccess = useCallback(() => toast.success(t('toast.updated')), [t]);
    const onError = useCallback(
        (error: Error) => toast.error(t('toast.updateFailed'), { description: error.message }),
        [t],
    );

    const isUpdatingMode = (() => {
        if (!updateGroup.isPending) return false;
        const variables = updateGroup.variables;
        return typeof variables === 'object' && variables !== null && 'mode' in variables;
    })();

    const priorityByItemId = useMemo(() => {
        const map = new Map<number, number>();
        (group.items ?? []).forEach((item) => {
            if (item.id !== undefined) map.set(item.id, item.priority);
        });
        return map;
    }, [group.items]);

    const clearWeightOverride = useCallback((id: string) => {
        setWeightOverrides((current) => {
            if (!(id in current)) return current;
            const next = { ...current };
            delete next[id];
            return next;
        });
    }, []);

    const handleDragStart = useCallback(() => {
        setMembers([...effectiveDisplayMembers]);
        setIsDragging(true);
    }, [effectiveDisplayMembers]);

    const handleDropReorder = useCallback(
        (nextMembers: SelectedMember[]) => {
            const itemsToUpdate = nextMembers
                .map((member, index) => ({ member, priority: index + 1 }))
                .filter(({ member, priority }) => {
                    if (!member.item_id) return false;
                    return priorityByItemId.get(member.item_id) !== priority;
                })
                .map(({ member, priority }) => ({
                    id: member.item_id!,
                    priority,
                    weight: member.weight ?? 1,
                }));

            if (itemsToUpdate.length > 0 && group.id) {
                updateGroup.mutate(
                    { id: group.id, items_to_update: itemsToUpdate },
                    { onSuccess, onError },
                );
            }
        },
        [group.id, onError, onSuccess, priorityByItemId, updateGroup],
    );

    const handleRemoveMember = useCallback(
        (id: string) => {
            const member = membersRef.current.find((item) => item.id === id);
            clearWeightOverride(id);
            if (member?.item_id !== undefined && group.id) {
                updateGroup.mutate(
                    { id: group.id, items_to_delete: [member.item_id] },
                    { onSuccess, onError },
                );
            }
        },
        [clearWeightOverride, group.id, onError, onSuccess, updateGroup],
    );

    const handleWeightChange = useCallback(
        (id: string, weight: number) => {
            setWeightOverrides((current) => ({ ...current, [id]: weight }));
            if (isDragging) {
                setMembers((current) => current.map((member) => (member.id === id ? { ...member, weight } : member)));
            }
            if (weightTimerRef.current) clearTimeout(weightTimerRef.current);
            weightTimerRef.current = setTimeout(() => {
                const member = membersRef.current.find((item) => item.id === id);
                if (!member?.item_id || !group.id) return;
                const priority = priorityByItemId.get(member.item_id);
                if (!priority) return;
                updateGroup.mutate(
                    { id: group.id, items_to_update: [{ id: member.item_id, priority, weight }] },
                    {
                        onSuccess: () => {
                            clearWeightOverride(id);
                            onSuccess();
                        },
                        onError,
                    },
                );
            }, 500);
        },
        [clearWeightOverride, group.id, isDragging, onError, onSuccess, priorityByItemId, updateGroup],
    );

    const handleSubmitEdit = useCallback(
        (values: GroupEditorValues, onDone?: () => void) => {
            if (!group.id) return;

            const originalItems = [...(group.items ?? [])].sort((left, right) => left.priority - right.priority);
            const originalById = new Map<number, { priority: number; weight: number }>();
            const originalIds = new Set<number>();
            originalItems.forEach((item) => {
                if (typeof item.id === 'number') {
                    originalIds.add(item.id);
                    originalById.set(item.id, { priority: item.priority, weight: item.weight });
                }
            });

            const nextIds = new Set<number>();
            values.members.forEach((member) => {
                if (typeof member.item_id === 'number') nextIds.add(member.item_id);
            });

            const itemsToDelete = Array.from(originalIds).filter((id) => !nextIds.has(id));
            const itemsToAdd = values.members
                .map((member, index) => ({ member, priority: index + 1 }))
                .filter(({ member }) => typeof member.item_id !== 'number')
                .map(({ member, priority }) => ({
                    channel_id: member.channel_id,
                    model_name: member.name,
                    priority,
                    weight: member.weight ?? 1,
                }));
            const itemsToUpdate = values.members
                .map((member, index) => ({ member, priority: index + 1 }))
                .filter(({ member }) => typeof member.item_id === 'number')
                .map(({ member, priority }) => {
                    const id = member.item_id!;
                    const original = originalById.get(id);
                    const weight = member.weight ?? 1;
                    if (!original || (original.priority === priority && original.weight === weight)) return null;
                    return { id, priority, weight };
                })
                .filter((item): item is { id: number; priority: number; weight: number } => item !== null);

            const payload: GroupUpdateRequest = { id: group.id };
            const nextName = values.name.trim();
            const nextRegex = values.match_regex.trim();
            const nextFirstTokenTimeOut = values.first_token_time_out ?? 0;
            const nextSessionKeepTime = values.session_keep_time ?? 0;

            if (nextName && nextName !== group.name) payload.name = nextName;
            if (values.mode !== group.mode) payload.mode = values.mode;
            if (nextRegex !== (group.match_regex ?? '')) payload.match_regex = nextRegex;
            if (nextFirstTokenTimeOut !== (group.first_token_time_out ?? 0)) payload.first_token_time_out = nextFirstTokenTimeOut;
            if (nextSessionKeepTime !== (group.session_keep_time ?? 0)) payload.session_keep_time = nextSessionKeepTime;
            if (values.retry_enabled !== (group.retry_enabled ?? false)) payload.retry_enabled = values.retry_enabled;
            if (values.max_retries !== (group.max_retries ?? 3)) payload.max_retries = values.max_retries;
            if (itemsToAdd.length > 0) payload.items_to_add = itemsToAdd;
            if (itemsToUpdate.length > 0) payload.items_to_update = itemsToUpdate;
            if (itemsToDelete.length > 0) payload.items_to_delete = itemsToDelete;

            if (Object.keys(payload).length === 1) {
                onDone?.();
                return;
            }

            updateGroup.mutate(payload, {
                onSuccess: () => {
                    onSuccess();
                    onDone?.();
                },
                onError,
            });
        },
        [group, onError, onSuccess, updateGroup],
    );

    const handleTogglePin = useCallback(() => {
        if (!group.id || togglePin.isPending) return;
        togglePin.mutate(
            { groupID: group.id, pinned: !group.pinned },
            {
                onSuccess: () => toast.success(group.pinned ? t('toast.unpinned') : t('toast.pinned')),
                onError: (error) => toast.error(t('toast.pinFailed'), { description: error.message }),
            },
        );
    }, [group.id, group.pinned, t, togglePin]);

    return (
        <article className="page-card group/card relative overflow-hidden transition-shadow hover:shadow-md">
            <div
                role="button"
                tabIndex={0}
                aria-expanded={expanded}
                onClick={() => {
                    if (!isDragging) setExpanded((current) => !current);
                }}
                onKeyDown={(event) => {
                    if ((event.key === 'Enter' || event.key === ' ') && !isDragging) {
                        event.preventDefault();
                        setExpanded((current) => !current);
                    }
                }}
                className="flex w-full cursor-pointer items-center gap-3 px-4 py-3 text-left md:py-4"
            >
                <div className="flex min-w-0 flex-1 items-center gap-3">
                    <span className="flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-full border border-border/40 bg-muted/30">
                        {GroupAvatar ? <GroupAvatar size={34} shape="circle" /> : <Waves className="size-4 text-muted-foreground" />}
                    </span>
                    <div className="min-w-0 flex-1">
                        <div className="flex min-w-0 items-center gap-1.5">
                            <h3 className="truncate text-sm font-semibold text-card-foreground md:text-base">{group.name}</h3>
                            {group.pinned ? <Pin className="size-3 shrink-0 fill-current text-primary" /> : null}
                        </div>
                        <p className="truncate text-xs text-muted-foreground">
                            {t(`mode.${MODE_LABELS[group.mode]}`)}
                            {firstModelName ? ` · ${firstModelName}` : ''}
                            {' · '}
                            {t('card.modelCount', { count: displayMembers.length })}
                        </p>
                    </div>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                    <motion.span
                        aria-hidden="true"
                        animate={{ rotate: expanded ? 180 : 0 }}
                        transition={{ duration: 0.2, ease: 'easeInOut' }}
                        className="shrink-0"
                    >
                        <ChevronDown className="size-4 text-muted-foreground" />
                    </motion.span>
                </div>
                <span className="sr-only">{expanded ? t('card.collapseModels') : t('card.expandModels')}</span>
            </div>

            <AnimatePresence initial={false}>
                {expanded ? (
                    <motion.div
                        key="expanded-content"
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: 'auto', opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ height: { duration: 0.25, ease: 'easeInOut' }, opacity: { duration: 0.2 } }}
                        className="overflow-hidden"
                    >
                        <div className="space-y-3 border-t border-border/40 px-4 pb-4 pt-3">
                            <div className="grid grid-cols-2 gap-1.5 sm:grid-cols-3">
                                {([GroupMode.RoundRobin, GroupMode.Failover, GroupMode.Weighted] as const).map((mode) => (
                                    <button
                                        key={mode}
                                        type="button"
                                        aria-pressed={group.mode === mode}
                                        aria-disabled={isUpdatingMode || !group.id}
                                        onClick={() => {
                                            if (isUpdatingMode || !group.id || mode === group.mode) return;
                                            updateGroup.mutate({ id: group.id, mode }, { onSuccess, onError });
                                        }}
                                        className={cn(
                                            'rounded-md border px-2 py-1.5 text-[10px] font-medium transition-[transform,border-color,background-color] duration-300 md:px-3 md:py-2 md:text-xs',
                                            group.mode === mode
                                                ? 'border-primary/20 bg-primary text-primary-foreground'
                                                : 'border-border/25 bg-card text-foreground hover:-translate-y-0.5 hover:border-primary/20 hover:bg-muted/25',
                                            !group.id && 'cursor-not-allowed opacity-50',
                                        )}
                                    >
                                        {t(`mode.${MODE_LABELS[mode]}`)}
                                    </button>
                                ))}
                            </div>

                            <section
                                className={cn(
                                    'relative overflow-hidden rounded-lg border border-border/25 bg-card',
                                    renderedMembers.length === 0 && 'min-h-28',
                                )}
                            >
                                <div className={cn(needsScroll && 'max-h-[248px] overflow-y-auto')}>
                                    <MemberList
                                        members={renderedMembers}
                                        onReorder={setMembers}
                                        onRemove={handleRemoveMember}
                                        onWeightChange={handleWeightChange}
                                        onDragStart={handleDragStart}
                                        onDrop={handleDropReorder}
                                        onDragFinish={() => setIsDragging(false)}
                                        autoScrollOnAdd={false}
                                        showWeight={group.mode === GroupMode.Weighted}
                                        dropScope={`list-item-${group.id ?? 'unknown'}`}
                                    />
                                </div>
                            </section>

                            <div className="flex items-center gap-1 border-t border-border/25 pt-2">
                                <MorphingDialog>
                                    <MorphingDialogTrigger
                                        aria-label={t('detail.actions.edit')}
                                        className="rounded-md p-2 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                                    >
                                        <Tooltip side="top" sideOffset={10} align="center">
                                            <TooltipTrigger asChild>
                                                <Pencil className="size-4" />
                                            </TooltipTrigger>
                                            <TooltipContent>{t('detail.actions.edit')}</TooltipContent>
                                        </Tooltip>
                                    </MorphingDialogTrigger>
                                    <MorphingDialogContainer>
                                        <MorphingDialogContent className="relative flex h-[calc(100dvh-2rem)] w-[min(100vw-2rem,92rem)] max-w-full flex-col overflow-hidden rounded-xl border border-border/35 bg-card px-4 py-4 text-card-foreground shadow-md md:h-[calc(100dvh-3rem)] md:px-6">
                                            <EditDialogContent
                                                group={group}
                                                displayMembers={renderedMembers}
                                                isSubmitting={updateGroup.isPending}
                                                onSubmit={handleSubmitEdit}
                                            />
                                        </MorphingDialogContent>
                                    </MorphingDialogContainer>
                                </MorphingDialog>

                                <Tooltip side="top" sideOffset={10} align="center">
                                    <TooltipTrigger>
                                        <CopyIconButton
                                            text={group.name}
                                            className="rounded-md p-2 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                                            copyIconClassName="size-4"
                                            checkIconClassName="size-4 text-primary"
                                        />
                                    </TooltipTrigger>
                                    <TooltipContent>{t('detail.actions.copyName')}</TooltipContent>
                                </Tooltip>

                                <PresetPopover group={group} />

                                <Tooltip side="top" sideOffset={10} align="center">
                                    <TooltipTrigger asChild>
                                        <button
                                            type="button"
                                            disabled={!group.id || togglePin.isPending}
                                            onClick={handleTogglePin}
                                            className={cn(
                                                'rounded-md p-2 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:pointer-events-none disabled:opacity-50',
                                                group.pinned && 'text-primary',
                                            )}
                                        >
                                            {group.pinned ? <Pin className="size-4 fill-current" /> : <PinOff className="size-4" />}
                                        </button>
                                    </TooltipTrigger>
                                    <TooltipContent>{group.pinned ? t('pin.unpin') : t('pin.pin')}</TooltipContent>
                                </Tooltip>

                                <div className="relative ml-auto">
                                    {!confirmDelete ? (
                                        <Tooltip side="top" sideOffset={10} align="center">
                                            <TooltipTrigger asChild>
                                                <button
                                                    type="button"
                                                    onClick={() => setConfirmDelete(true)}
                                                    className="rounded-md p-2 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
                                                >
                                                    <Trash2 className="size-4" />
                                                </button>
                                            </TooltipTrigger>
                                            <TooltipContent>{t('detail.actions.delete')}</TooltipContent>
                                        </Tooltip>
                                    ) : null}

                                    {confirmDelete ? (
                                        <div className={cn('absolute inset-y-0 right-0 flex items-center gap-2 rounded-lg bg-destructive px-2', OVERLAY_ENTRANCE_FROM_RIGHT)}>
                                            <button
                                                type="button"
                                                onClick={() => setConfirmDelete(false)}
                                                className="flex size-7 items-center justify-center rounded-lg bg-destructive-foreground/20 text-destructive-foreground transition-colors hover:bg-destructive-foreground/30"
                                            >
                                                <X className="size-4" />
                                            </button>
                                            <button
                                                type="button"
                                                disabled={deleteGroup.isPending}
                                                onClick={() => {
                                                    if (!group.id) return;
                                                    deleteGroup.mutate(group.id, {
                                                        onSuccess: () => toast.success(t('toast.deleted')),
                                                        onError: (error) => toast.error(t('toast.deleteFailed'), { description: error.message }),
                                                    });
                                                }}
                                                className="flex h-7 items-center gap-2 rounded-lg bg-destructive-foreground px-3 text-sm font-semibold text-destructive transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
                                            >
                                                <Trash2 className="size-3.5" />
                                                {t('detail.actions.confirmDelete')}
                                            </button>
                                        </div>
                                    ) : null}
                                </div>
                            </div>
                        </div>
                    </motion.div>
                ) : null}
            </AnimatePresence>
        </article>
    );
}
