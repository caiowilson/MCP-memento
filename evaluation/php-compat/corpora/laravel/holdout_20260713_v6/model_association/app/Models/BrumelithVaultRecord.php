<?php

namespace HoldoutV6\Laravel\Brumelith\Models;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\HasMany;

final class BrumelithVaultRecord extends Model
{
    protected $table = 'brumelith_vault_records';

    public function brumelithFragments(): HasMany
    {
        return $this->hasMany(BrumelithFragmentRecord::class, 'brumelith_vault_key');
    }
}
