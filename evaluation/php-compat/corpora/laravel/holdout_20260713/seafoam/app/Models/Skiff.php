<?php
declare(strict_types=1);

namespace App\Models\Seafoam;

use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;

final class Skiff extends Model
{
    public function harbor(): BelongsTo
    {
        return $this->belongsTo(Harbor::class, 'harbor_id');
    }
}
