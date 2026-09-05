'use client';

import {
    type SiteChannelAccount,
    type SiteChannelGroup,
    useAddSiteManualModels,
    useCreateSiteChannelKey,
    useUpdateSiteProjectedChannelSettings,
} from '@/api/endpoints/site-channel';
import { useState } from 'react';
import { SiteChannelTableView } from './SiteChannelTable';
import { SiteChannelPendingJump } from './types';

import { AddManualModelsDialog } from './AddManualModelsDialog';
import { AdvancedSettingsDialog } from './AdvancedSettingsDialog';
import { CreateSiteKeyDialog } from './CreateSiteKeyDialog';
import { SiteAccountToolbar } from './SiteAccountToolbar';
import { SourceKeysDialog } from './SourceKeysDialog';
import { useSiteChannelModels } from './useSiteChannelModels';
export function SiteAccountPanel({
    siteId,
    account,
    accounts,
    activeAccountId,
    onSelectAccount,
    highlightedAccountId,
    registerAccountTabRef,
    jumpRequest,
    onJumpHandled,
    onNavigateToChannel,
}: {
    siteId: number;
    account: SiteChannelAccount;
    accounts: SiteChannelAccount[];
    activeAccountId: number | null;
    onSelectAccount: (accountId: number) => void;
    highlightedAccountId: number | null;
    registerAccountTabRef: (accountId: number, node: HTMLButtonElement | null) => void;
    jumpRequest: SiteChannelPendingJump | null;
    onJumpHandled: (requestId: number) => void;
    onNavigateToChannel: (channelId: number) => void;
}) {
    const [creatingGroup, setCreatingGroup] = useState<SiteChannelGroup | null>(null);
    const [editingProjectedGroup, setEditingProjectedGroup] = useState<SiteChannelGroup | null>(null);
    const [editingAdvancedGroup, setEditingAdvancedGroup] = useState<SiteChannelGroup | null>(null);
    const [addingManualGroup, setAddingManualGroup] = useState<SiteChannelGroup | null>(null);

    const createKeyMutation = useCreateSiteChannelKey(siteId, account.account_id);
    const advancedMutation = useUpdateSiteProjectedChannelSettings(siteId, account.account_id);
    const addManualModelsMutation = useAddSiteManualModels(siteId, account.account_id);
    const handleOpenCreateKey = (group: SiteChannelGroup) => setCreatingGroup(group);
    const handleOpenProjectedKeys = (group: SiteChannelGroup) => setEditingProjectedGroup(group);
    const handleOpenAdvancedSettings = (group: SiteChannelGroup) => setEditingAdvancedGroup(group);
    const handleOpenAddManualModels = (group: SiteChannelGroup) => setAddingManualGroup(group);

    const modelState = useSiteChannelModels({
        siteId,
        account,
        jumpRequest,
        onJumpHandled,
        pendingGroupChanges: advancedMutation.isPending || addManualModelsMutation.isPending,
    });
    const {
        pendingModelKeys,
        selectedModelKeys,
        highlightedModelKey,
        tableHandleRef,
        panelPreferences,
        visibleModels,
        modelsScopeKey,
        handleToggleModelSelection,
        handleToggleAllVisible,
        allVisibleSelected,
        applyRouteChange,
        handleDeleteManualModel,
        handleToggleDisabled,
        handleSortChange,
    } = modelState;
    const groupActions = {
        creatingGroup,
        isCreatingKey: createKeyMutation.isPending,
        handleOpenCreateKey,
        handleOpenProjectedKeys,
        handleOpenAdvancedSettings,
        handleOpenAddManualModels,
    };

    return (
        <div className="flex min-h-0 flex-1 flex-col gap-2.5">
            <SiteAccountToolbar
                account={account}
                accounts={accounts}
                activeAccountId={activeAccountId}
                onSelectAccount={onSelectAccount}
                highlightedAccountId={highlightedAccountId}
                registerAccountTabRef={registerAccountTabRef}
                modelState={modelState}
                groupActions={groupActions}
            />

            {creatingGroup && (
                <CreateSiteKeyDialog
                    creatingGroup={creatingGroup}
                    account={account}
                    createKeyMutation={createKeyMutation}
                    onClose={() => setCreatingGroup(null)}
                />
            )}

            {editingAdvancedGroup && (
                <AdvancedSettingsDialog
                    editingAdvancedGroup={editingAdvancedGroup}
                    advancedMutation={advancedMutation}
                    onClose={() => setEditingAdvancedGroup(null)}
                />
            )}

            {addingManualGroup && (
                <AddManualModelsDialog
                    addingManualGroup={addingManualGroup}
                    addManualModelsMutation={addManualModelsMutation}
                    onClose={() => setAddingManualGroup(null)}
                />
            )}

            {editingProjectedGroup && (
                <SourceKeysDialog
                    siteId={siteId}
                    editingProjectedGroup={editingProjectedGroup}
                    account={account}
                    onClose={() => setEditingProjectedGroup(null)}
                />
            )}

            {visibleModels.length === 0 ? (
                <div className="page-empty-state flex min-h-[18rem] flex-1 items-center justify-center bg-muted/20 text-sm">
                    当前筛选和搜索条件下没有匹配模型
                </div>
            ) : (
                <div className="page-card flex min-h-0 flex-1 flex-col overflow-hidden border-border/70 bg-card/70">
                    <SiteChannelTableView
                        ref={tableHandleRef}
                        models={visibleModels}
                        resetKey={modelsScopeKey}
                        allVisibleSelected={allVisibleSelected}
                        pendingModelKeys={pendingModelKeys}
                        selectedModelKeys={selectedModelKeys}
                        compactMode={panelPreferences.compactMode}
                        tableSort={panelPreferences.tableSort}
                        highlightedModelKey={highlightedModelKey}
                        onToggleModelSelection={handleToggleModelSelection}
                        onToggleAllVisible={handleToggleAllVisible}
                        onSortChange={handleSortChange}
                        onMoveModel={(model, nextRouteType) => applyRouteChange([model], nextRouteType)}
                        onToggleDisabled={handleToggleDisabled}
                        onDeleteManualModel={handleDeleteManualModel}
                        onNavigateToChannel={onNavigateToChannel}
                    />
                </div>
            )}
        </div>
    );
}
