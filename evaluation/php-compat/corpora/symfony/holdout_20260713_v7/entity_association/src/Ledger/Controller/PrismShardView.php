<?php
declare(strict_types=1);

namespace VirelliumK9\Ledger\Controller;

use VirelliumK9\Ledger\Entity\PrismShard;

final class PrismShardView
{
    public function vaultCode(PrismShard $shard): string
    {
        return $shard->vault()?->code() ?? 'unassigned';
    }
}
