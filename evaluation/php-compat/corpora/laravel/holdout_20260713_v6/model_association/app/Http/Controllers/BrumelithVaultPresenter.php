<?php

namespace HoldoutV6\Laravel\Brumelith\Http\Controllers;

use HoldoutV6\Laravel\Brumelith\Models\BrumelithVaultRecord;

final class BrumelithVaultPresenter
{
    public function presentBrumelithVault(BrumelithVaultRecord $brumelithVault): array
    {
        return [
            'vault_key' => $brumelithVault->getKey(),
            'fragment_codes' => $brumelithVault->brumelithFragments()
                ->orderBy('etched_at')
                ->pluck('fragment_code')
                ->all(),
        ];
    }
}
